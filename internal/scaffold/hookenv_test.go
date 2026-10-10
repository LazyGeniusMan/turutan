// SPDX-License-Identifier: Apache-2.0

package scaffold

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHookEnvRedactsSecrets(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "hook-canary-token")
	t.Setenv("TURUTAN_SSH_KEY", "hook-canary-key")
	t.Setenv("TURUTAN_SSH_PASSWORD", "hook-canary-pass")
	target := t.TempDir()
	var stderr strings.Builder
	hook := `env | sort`
	if err := runHookCmd(target, hook, &stderr); err != nil {
		t.Fatalf("runHookCmd error: %v", err)
	}
	out := stderr.String()
	for _, canary := range []string{
		"hook-canary-token",
		"hook-canary-key",
		"hook-canary-pass",
	} {
		if strings.Contains(out, canary) {
			t.Errorf("hook env leaked secret %q in output", canary)
		}
	}
	if !strings.Contains(out, "PATH=") {
		t.Errorf("hook env missing PATH, want minimal functional env: %q", out)
	}
}

func TestMigrationEnvRedactsSecretsKeepsIdentity(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "mig-canary-token")
	target := t.TempDir()
	var stderr strings.Builder
	oldID := strings.Repeat("a", 40)
	newID := strings.Repeat("b", 40)
	cmd := `env | sort`
	if err := runMigrationCmd(
		target, cmd, oldID, newID, &stderr,
	); err != nil {
		t.Fatalf("runMigrationCmd error: %v", err)
	}
	out := stderr.String()
	if strings.Contains(out, "mig-canary-token") {
		t.Errorf("migration env leaked GITHUB_TOKEN")
	}
	if !strings.Contains(out, "TURUTAN_OLD="+oldID) {
		t.Errorf("migration env missing TURUTAN_OLD: %q", out)
	}
	if !strings.Contains(out, "TURUTAN_NEW="+newID) {
		t.Errorf("migration env missing TURUTAN_NEW: %q", out)
	}
}

func TestMissingKeyTyped(t *testing.T) {
	src := makeTemplate(
		t, "", map[string]string{"f.txt.tmpl": "{{.absent_key}}\n"},
	)
	target := filepath.Join(t.TempDir(), "proj")
	opts := testOptions(&strings.Builder{})
	err := Bootstrap(context.Background(), src, target, opts)
	if err == nil {
		t.Fatal("Bootstrap succeeded, want missing-key error")
	}
	key, ok := AsMissingKey(err)
	if !ok {
		t.Fatalf("error is not typed missing-key: %T %v", err, err)
	}
	if key != "absent_key" {
		t.Errorf("missing key = %q, want absent_key", key)
	}
	_ = os.Getenv("unused")
}
