// SPDX-License-Identifier: Apache-2.0

package git

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/go-git/go-git/v6/plumbing/client"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/plumbing/transport/http"
	"github.com/go-git/go-git/v6/plumbing/transport/ssh"
	gossh "golang.org/x/crypto/ssh"
)

// Environment knobs for git transport authentication (spec §10). All
// credentials arrive via the environment only: they are never read from
// config files and never logged (see redacted).
const (
	// EnvSSHKey holds an SSH private key: either PEM content or a path to
	// a PEM file. It wins over EnvSSHPassword and the ssh-agent.
	EnvSSHKey = "TURUTAN_SSH_KEY"
	// EnvSSHPassword holds the SSH password. It wins over the ssh-agent.
	EnvSSHPassword = "TURUTAN_SSH_PASSWORD" // gitleaks:allow (env var name, not a credential)
	// EnvGitHubToken holds an HTTPS token used as x-access-token
	// credentials on http(s) remotes.
	EnvGitHubToken = "GITHUB_TOKEN"
	// EnvInsecureSkipVerify disables SSH known_hosts and TLS verification.
	// Strict verification is the default; setting this prints a loud
	// warning on every use.
	EnvInsecureSkipVerify = "TURUTAN_INSECURE_SKIP_VERIFY"
)

// InsecureWarning is the loud opt-out notice emitted on stderr whenever
// EnvInsecureSkipVerify disables verification.
const InsecureWarning = "turutan: WARNING: TURUTAN_INSECURE_SKIP_VERIFY is set: SSH host-key and TLS verification are DISABLED; connections are vulnerable to man-in-the-middle attacks"

// InsecureSkipVerify reports whether verification is disabled via the
// environment. Empty, "0", "false", "no" and "off" all mean strict
// (the default); anything else opts out.
func InsecureSkipVerify() bool {
	v, ok := os.LookupEnv(EnvInsecureSkipVerify)
	if !ok {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

// isSSHURL reports whether raw addresses an SSH remote: an ssh:// or
// git:// scheme, or an scp-like user@host:path locator.
func isSSHURL(raw string) bool {
	if strings.HasPrefix(raw, "ssh://") || strings.HasPrefix(raw, "git://") {
		return true
	}
	return !strings.Contains(raw, "://") && scpLikeURL(raw)
}

// scpLikeURL matches scp-like SSH locators without "://".
func scpLikeURL(s string) bool {
	if strings.Contains(s, "://") {
		return false
	}
	at := strings.Index(s, "@")
	colon := strings.Index(s, ":")
	return at > 0 && colon > at+1
}

// isHTTPURL reports whether raw is an http(s) remote.
func isHTTPURL(raw string) bool {
	return strings.HasPrefix(raw, "https://") || strings.HasPrefix(raw, "http://")
}

// sshUser extracts the SSH username from raw: the userinfo or scp-like
// user, defaulting to "git" (go-git ssh.DefaultUsername).
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

// ClientOptions resolves transport authentication for raw from the
// environment: SSH remotes try TURUTAN_SSH_KEY, then
// TURUTAN_SSH_PASSWORD, then the ssh-agent; http(s) remotes use
// GITHUB_TOKEN as x-access-token credentials when set. Local paths and
// file:// URLs need no transport and return nil. The second result
// reports whether verification is disabled so callers can warn loudly.
// Errors never echo credential values.
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

// httpClientOptions builds options for http(s) remotes: token credentials
// when GITHUB_TOKEN is set, plus insecure TLS on opt-out.
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

// sshClientOptions builds options for SSH remotes following the key >
// password > agent precedence. Host-key verification stays strict
// (known_hosts) unless the insecure opt-out wraps the auth to ignore it.
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
	}
	return []client.Option{client.WithSSHAuth(auth)}, nil
}

// sshKeyBytes resolves an EnvSSHKey value to PEM bytes: when the value
// names an existing file its content is read, otherwise the value itself
// must be the PEM content.
func sshKeyBytes(value string) ([]byte, error) {
	if info, err := os.Stat(value); err == nil && !info.IsDir() {
		pem, err := os.ReadFile(value)
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
	if len(value) < 64 {
		return nil, fmt.Errorf("value is neither an existing key file nor PEM content")
	}
	return []byte(value), nil
}

// insecureHostKeyAuth wraps an SSH auth to ignore host-key verification.
// It exists only for the TURUTAN_INSECURE_SKIP_VERIFY opt-out, which
// always pairs with the loud InsecureWarning.
type insecureHostKeyAuth struct {
	inner client.SSHAuth
}

// ClientConfig delegates to the wrapped auth then disables host-key
// verification on the resulting config.
func (a insecureHostKeyAuth) ClientConfig(ctx context.Context, req *transport.Request) (*gossh.ClientConfig, error) {
	cfg, err := a.inner.ClientConfig(ctx, req)
	if err != nil {
		return nil, err
	}
	cfg.HostKeyCallback = gossh.InsecureIgnoreHostKey()
	return cfg, nil
}

// warnInsecure emits the loud opt-out warning. Transport helpers call it
// whenever ClientOptions reports insecure use.
func warnInsecure() {
	fmt.Fprintln(os.Stderr, InsecureWarning)
}
