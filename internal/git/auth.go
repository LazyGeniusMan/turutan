// SPDX-License-Identifier: Apache-2.0

package git

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-git/go-git/v6/plumbing/client"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/plumbing/transport/http"
	"github.com/go-git/go-git/v6/plumbing/transport/ssh"
	gossh "golang.org/x/crypto/ssh"
)

const (
	EnvSSHKey             = "TURUTAN_SSH_KEY"
	EnvSSHPassword        = "TURUTAN_SSH_PASSWORD" // #nosec G101 -- env var name, not a hardcoded credential (betterleaks:allow)
	EnvGitHubToken        = "GITHUB_TOKEN"         // #nosec G101 -- env var name, not a hardcoded credential
	EnvInsecureSkipVerify = "TURUTAN_INSECURE_SKIP_VERIFY"
	EnvKnownHosts         = "TURUTAN_KNOWN_HOSTS"
)

const InsecureWarning = "turutan: WARNING: " +
	"TURUTAN_INSECURE_SKIP_VERIFY is set: SSH host-key and TLS " +
	"verification are DISABLED; connections are vulnerable to " +
	"man-in-the-middle attacks"

func InsecureSkipVerify() bool {
	v, ok := os.LookupEnv(EnvInsecureSkipVerify)
	if !ok {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func isSSHURL(raw string) bool {
	if strings.HasPrefix(raw, "ssh://") || strings.HasPrefix(raw, "git://") {
		return true
	}
	return !strings.Contains(raw, "://") && IsScpLike(raw)
}

func IsScpLike(s string) bool {
	if strings.Contains(s, "://") {
		return false
	}
	at := strings.Index(s, "@")
	colon := strings.Index(s, ":")
	if at <= 0 || colon <= at+1 || colon >= len(s)-1 {
		return false
	}
	return !strings.Contains(s[:at], "/")
}

func isHTTPURL(raw string) bool {
	return strings.HasPrefix(raw, "https://") || strings.HasPrefix(raw, "http://")
}

func sshUser(raw string) string {
	if strings.Contains(raw, "://") {
		if parsed, err := url.Parse(raw); err == nil && parsed.User != nil {
			if name := parsed.User.Username(); name != "" {
				return name
			}
		}
		return ssh.DefaultUsername
	}
	if at := strings.Index(raw, "@"); at > 0 {
		return raw[:at]
	}
	return ssh.DefaultUsername
}

func ClientOptions(raw string) ([]client.Option, bool, error) {
	insecure := InsecureSkipVerify()
	if !isSSHURL(raw) && !isHTTPURL(raw) {
		return nil, insecure, nil
	}
	if isHTTPURL(raw) {
		return httpClientOptions(insecure), insecure, nil
	}
	opts, err := sshClientOptions(raw, insecure)
	if err != nil {
		return nil, insecure, err
	}
	return opts, insecure, nil
}

func httpClientOptions(insecure bool) []client.Option {
	var opts []client.Option
	if token := os.Getenv(EnvGitHubToken); token != "" {
		opts = append(opts, client.WithHTTPAuth(&http.BasicAuth{Username: "x-access-token", Password: token}))
	}
	if insecure {
		opts = append(opts, client.WithInsecureSkipTLS())
	}
	return opts
}

func sshClientOptions(raw string, insecure bool) ([]client.Option, error) {
	user := sshUser(raw)
	var auth client.SSHAuth
	if key, ok := os.LookupEnv(EnvSSHKey); ok && strings.TrimSpace(key) != "" {
		pem, err := sshKeyBytes(strings.TrimSpace(key))
		if err != nil {
			return nil, fmt.Errorf("reading SSH key from %s: %w", EnvSSHKey, err)
		}
		public, err := ssh.NewPublicKeys(user, pem, "")
		if err != nil {
			return nil, fmt.Errorf("parsing SSH key from %s: %w", EnvSSHKey, err)
		}
		auth = public
	} else if password, ok := os.LookupEnv(EnvSSHPassword); ok && password != "" {
		auth = &ssh.Password{User: user, Password: password}
	} else {
		agent, err := ssh.NewSSHAgentAuth(user)
		if err != nil {
			return nil, fmt.Errorf("using ssh-agent for %q: %w", redacted(raw), err)
		}
		auth = agent
	}
	if insecure {
		auth = insecureHostKeyAuth{inner: auth}
	} else if files, err := knownHostsFiles(); err != nil {
		return nil, err
	} else if files != nil {
		callback, err := ssh.NewKnownHostsCallback(files...)
		if err != nil {
			return nil, fmt.Errorf("reading known_hosts from %s: %w", EnvKnownHosts, err)
		}
		auth = knownHostsAuth{inner: auth, callback: callback}
	}
	return []client.Option{client.WithSSHAuth(auth)}, nil
}

const minKeyMaterialLen = 64

func sshKeyBytes(value string) ([]byte, error) {
	if info, err := os.Stat(value); err == nil && !info.IsDir() {
		pem, err := os.ReadFile(value) // #nosec G304 -- reads SSH key file named by TURUTAN_SSH_KEY env; content never logged
		if err != nil {
			return nil, fmt.Errorf("reading key file: %w", err)
		}
		return pem, nil
	}
	if strings.Contains(value, "-----BEGIN") {
		return []byte(value), nil
	}
	if _, err := strconv.Atoi(value); err == nil {
		return nil, fmt.Errorf("value looks like neither a key file nor PEM content")
	}
	if len(value) < minKeyMaterialLen {
		return nil, fmt.Errorf("value is neither an existing key file nor PEM content")
	}
	return []byte(value), nil
}

func knownHostsFiles() ([]string, error) {
	raw, ok := os.LookupEnv(EnvKnownHosts)
	if !ok || strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	path := strings.TrimSpace(raw)
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s %q: %w", EnvKnownHosts, path, err)
	}
	if !info.IsDir() {
		return []string{path}, nil
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s %q: %w", EnvKnownHosts, path, err)
	}
	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if info, err := entry.Info(); err != nil || !info.Mode().IsRegular() {
			continue
		}
		files = append(files, filepath.Join(path, entry.Name()))
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("reading %s %q: directory holds no key files", EnvKnownHosts, path)
	}
	return files, nil
}

type insecureHostKeyAuth struct {
	inner client.SSHAuth
}

func (a insecureHostKeyAuth) ClientConfig(ctx context.Context, req *transport.Request) (*gossh.ClientConfig, error) {
	cfg, err := a.inner.ClientConfig(ctx, req)
	if err != nil {
		return nil, err
	}
	cfg.HostKeyCallback = gossh.InsecureIgnoreHostKey() // #nosec G106 -- explicit TURUTAN_INSECURE_SKIP_VERIFY opt-out with loud warning; strict known_hosts is default
	return cfg, nil
}

type knownHostsAuth struct {
	inner    client.SSHAuth
	callback gossh.HostKeyCallback
}

func (a knownHostsAuth) ClientConfig(ctx context.Context, req *transport.Request) (*gossh.ClientConfig, error) {
	cfg, err := a.inner.ClientConfig(ctx, req)
	if err != nil {
		return nil, err
	}
	cfg.HostKeyCallback = a.callback
	return cfg, nil
}

func warnInsecure() {
	fmt.Fprintln(os.Stderr, InsecureWarning)
}
