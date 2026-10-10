// SPDX-License-Identifier: Apache-2.0

package git

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gossh "golang.org/x/crypto/ssh"
)

func clearAuthEnv(t *testing.T) {
	t.Helper()
	t.Setenv(EnvSSHKey, "")
	t.Setenv(EnvSSHPassword, "")
	t.Setenv(EnvGitHubToken, "")
	t.Setenv(EnvInsecureSkipVerify, "")
	t.Setenv(EnvKnownHosts, "")
	os.Unsetenv(EnvSSHKey)
	os.Unsetenv(EnvSSHPassword)
	os.Unsetenv(EnvGitHubToken)
	os.Unsetenv(EnvInsecureSkipVerify)
	os.Unsetenv(EnvKnownHosts)
}

func testKeyPEM(t *testing.T) string {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

func TestInsecureSkipVerify(t *testing.T) {
	tests := []struct {
		name  string
		value *string
		want  bool
	}{
		{name: "unset means strict", value: nil, want: false},
		{name: "empty means strict", value: new(""), want: false},
		{name: "zero means strict", value: new("0"), want: false},
		{name: "false means strict", value: new("false"), want: false},
		{name: "no means strict", value: new("no"), want: false},
		{name: "off means strict", value: new("off"), want: false},
		{name: "one opts out", value: new("1"), want: true},
		{name: "true opts out", value: new("true"), want: true},
		{name: "yes opts out", value: new("yes"), want: true},
		{name: "on opts out", value: new("on"), want: true},
		{name: "ON opts out case-insensitive", value: new("ON"), want: true},
		{name: "True opts out case-insensitive", value: new("True"), want: true},
		{name: "YES opts out case-insensitive", value: new("YES"), want: true},
		{name: "padded true opts out", value: new("  true  "), want: true},
		{name: "arbitrary string stays strict", value: new("banana"), want: false},
		{name: "two stays strict", value: new("2"), want: false},
		{name: "enabled stays strict", value: new("enabled"), want: false},
		{name: "y stays strict", value: new("y"), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearAuthEnv(t)
			if tt.value != nil {
				t.Setenv(EnvInsecureSkipVerify, *tt.value)
			}
			if got := InsecureSkipVerify(); got != tt.want {
				t.Errorf("InsecureSkipVerify() = %v, want %v", got, tt.want)
			}
		})
	}
}

//go:fix inline
func strPtr(s string) *string { return new(s) }

func TestClientOptionsMatrix(t *testing.T) {
	pemContent := testKeyPEM(t)
	keyFile := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(keyFile, []byte(pemContent), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name      string
		url       string
		env       map[string]string
		wantOpts  bool
		wantInsec bool
		wantErr   bool
	}{
		{
			name:     "local path needs no transport",
			url:      t.TempDir(),
			env:      map[string]string{},
			wantOpts: false,
		},
		{
			name:     "file URL needs no transport",
			url:      "file:///tmp/repo",
			env:      map[string]string{},
			wantOpts: false,
		},
		{
			name:     "https without token has no auth",
			url:      "https://github.com/org/web.git",
			env:      map[string]string{},
			wantOpts: false,
		},
		{
			name:     "https with token authenticates",
			url:      "https://github.com/org/web.git",
			env:      map[string]string{EnvGitHubToken: "example-token-value"},
			wantOpts: true,
		},
		{
			name:     "ssh with key content authenticates",
			url:      "git@github.com:org/web.git",
			env:      map[string]string{EnvSSHKey: pemContent},
			wantOpts: true,
		},
		{
			name:     "ssh with key file authenticates",
			url:      "ssh://git@github.com/org/web.git",
			env:      map[string]string{EnvSSHKey: keyFile},
			wantOpts: true,
		},
		{
			name:     "ssh with password authenticates",
			url:      "git@github.com:org/web.git",
			env:      map[string]string{EnvSSHPassword: "s3cr3t"}, // betterleaks:allow (test fixture, not a credential)
			wantOpts: true,
		},
		{
			name:     "key wins over password",
			url:      "git@github.com:org/web.git",
			env:      map[string]string{EnvSSHKey: pemContent, EnvSSHPassword: "s3cr3t"}, // betterleaks:allow (test fixture, not a credential)
			wantOpts: true,
		},
		{
			name:    "garbage key fails closed",
			url:     "git@github.com:org/web.git",
			env:     map[string]string{EnvSSHKey: "not-a-key-at-all"},
			wantErr: true,
		},
		{
			name:      "insecure flag reported for https",
			url:       "https://github.com/org/web.git",
			env:       map[string]string{EnvInsecureSkipVerify: "1"},
			wantOpts:  true,
			wantInsec: true,
		},
		{
			name:      "insecure flag reported for ssh with password",
			url:       "git@github.com:org/web.git",
			env:       map[string]string{EnvSSHPassword: "s3cr3t", EnvInsecureSkipVerify: "true"}, // betterleaks:allow (test fixture, not a credential)
			wantOpts:  true,
			wantInsec: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearAuthEnv(t)
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			opts, insecure, err := ClientOptions(tt.url)
			if tt.wantErr {
				if err == nil {
					t.Fatal("ClientOptions succeeded, want error")
				}
				for _, secret := range []string{"s3cr3t", "not-a-key-at-all", "example-token-value"} { // betterleaks:allow (test fixture, not a credential)
					if strings.Contains(err.Error(), secret) {
						t.Errorf("error leaks a credential: %q", err.Error())
					}
				}
				return
			}
			if err != nil {
				if len(tt.env) == 0 || tt.env[EnvInsecureSkipVerify] != "" && len(tt.env) == 1 {
					t.Logf("no auth configured, agent unavailable: %v", err)
					return
				}
				t.Fatalf("ClientOptions error: %v", err)
			}
			if tt.wantOpts && len(opts) == 0 {
				t.Error("ClientOptions returned no options, want auth")
			}
			if !tt.wantOpts && len(opts) != 0 && tt.env[EnvGitHubToken] == "" {
				t.Errorf("ClientOptions returned %d options, want none", len(opts))
			}
			if insecure != tt.wantInsec {
				t.Errorf("insecure = %v, want %v", insecure, tt.wantInsec)
			}
		})
	}
}

func TestKnownHosts(t *testing.T) {
	t.Run("missing path fails closed", func(t *testing.T) {
		clearAuthEnv(t)
		t.Setenv(EnvSSHPassword, "s3cr3t")
		t.Setenv(EnvKnownHosts, filepath.Join(t.TempDir(), "no-such-file"))
		if _, _, err := ClientOptions("git@github.com:org/web.git"); err == nil {
			t.Error("ClientOptions with missing known_hosts succeeded, want error")
		}
	})
	t.Run("empty directory fails closed", func(t *testing.T) {
		clearAuthEnv(t)
		t.Setenv(EnvSSHPassword, "s3cr3t")
		t.Setenv(EnvKnownHosts, t.TempDir())
		if _, _, err := ClientOptions("git@github.com:org/web.git"); err == nil {
			t.Error("ClientOptions with empty known_hosts dir succeeded, want error")
		}
	})
	t.Run("file override honored", func(t *testing.T) {
		clearAuthEnv(t)
		t.Setenv(EnvSSHPassword, "s3cr3t")
		t.Setenv(EnvKnownHosts, testKnownHostsFile(t, "github.com"))
		opts, insecure, err := ClientOptions("git@github.com:org/web.git")
		if err != nil {
			t.Fatalf("ClientOptions error: %v", err)
		}
		if len(opts) == 0 {
			t.Error("ClientOptions returned no options, want SSH auth")
		}
		if insecure {
			t.Error("insecure = true, want strict with known_hosts override")
		}
	})
	t.Run("directory override honored", func(t *testing.T) {
		clearAuthEnv(t)
		t.Setenv(EnvSSHPassword, "s3cr3t")
		dir := t.TempDir()
		data, err := os.ReadFile(testKnownHostsFile(t, "github.com"))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "known_hosts"), data, 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv(EnvKnownHosts, dir)
		if _, _, err := ClientOptions("git@github.com:org/web.git"); err != nil {
			t.Fatalf("ClientOptions error: %v", err)
		}
	})
	t.Run("unset keeps strict default", func(t *testing.T) {
		clearAuthEnv(t)
		t.Setenv(EnvSSHPassword, "s3cr3t")
		if _, insecure, err := ClientOptions("git@github.com:org/web.git"); err != nil || insecure {
			t.Errorf("ClientOptions = (_, %v, %v); want strict defaults", insecure, err)
		}
	})
}

func testKnownHostsFile(t *testing.T, host string) string {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := gossh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	line := fmt.Sprintf("%s %s", host, strings.TrimSpace(string(gossh.MarshalAuthorizedKey(key))))
	path := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(path, []byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAuthRedaction(t *testing.T) {
	t.Run("token URL redacted", func(t *testing.T) {
		user, pass, host := "x-access-token", "example-token-value", "github.com/org/web.git"
		raw := "https://" + user + ":" + pass + "@" + host
		got := redacted(raw)
		if strings.Contains(got, pass) {
			t.Errorf("redacted URL leaks the token: %q", got)
		}
		if !strings.Contains(got, "github.com/org/web.git") {
			t.Errorf("redacted URL lost the path: %q", got)
		}
	})
	t.Run("password URL redacted", func(t *testing.T) {
		user, pass, host := "user", "s3cr3t", "example.com/org/web.git" // #nosec G101 -- test fixture (betterleaks:allow)
		got := redacted("https://" + user + ":" + pass + "@" + host)
		if strings.Contains(got, pass) {
			t.Errorf("redacted URL leaks the password: %q", got)
		}
	})
	t.Run("scp-like keeps identity only", func(t *testing.T) {
		got := redacted("git@github.com:org/web.git")
		if got != "git@github.com:org/web.git" {
			t.Errorf("redacted = %q, want unchanged scp-like URL", got)
		}
	})
	t.Run("insecure warning is loud", func(t *testing.T) {
		if !strings.Contains(InsecureWarning, "WARNING") || !strings.Contains(InsecureWarning, EnvInsecureSkipVerify) {
			t.Errorf("warning is not loud/explicit: %q", InsecureWarning)
		}
	})
}

func TestSSHUser(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{name: "scp-like user", url: "deploy@github.com:org/web.git", want: "deploy"},
		{name: "scp-like default", url: "git@github.com:org/web.git", want: "git"},
		{name: "ssh scheme userinfo", url: "ssh://deploy@example.com/org/web.git", want: "deploy"},
		{name: "ssh scheme default", url: "ssh://example.com/org/web.git", want: "git"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sshUser(tt.url); got != tt.want {
				t.Errorf("sshUser(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}
