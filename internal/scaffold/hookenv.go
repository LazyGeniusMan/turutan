// SPDX-License-Identifier: Apache-2.0

package scaffold

import (
	"os"
	"strings"
)

var hookSecretEnv = []string{
	"GITHUB_TOKEN",
	"TURUTAN_SSH_KEY",
	"TURUTAN_SSH_PASSWORD",
}

func hookEnv(extra ...string) []string {
	blocked := make(map[string]bool, len(hookSecretEnv))
	for _, key := range hookSecretEnv {
		blocked[key] = true
	}
	env := make([]string, 0, len(os.Environ())+len(extra))
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		if blocked[key] {
			continue
		}
		env = append(env, kv)
	}
	return append(env, extra...)
}
