// SPDX-License-Identifier: Apache-2.0

package scaffold

import (
	"bytes"
	"strings"
	"testing"
)

func TestConflictModeValidate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		mode    ConflictMode
		wantErr bool
	}{
		{name: "empty uses default", mode: "", wantErr: false},
		{name: "inline", mode: ConflictInline, wantErr: false},
		{name: "rej", mode: ConflictRej, wantErr: false},
		{name: "unknown mode", mode: ConflictMode("merge"), wantErr: true},
		{name: "case sensitive", mode: ConflictMode("Inline"), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := tt.mode.Validate(); (err != nil) != tt.wantErr {
				t.Errorf("Validate(%q) err = %v, wantErr %v", string(tt.mode), err, tt.wantErr)
			}
		})
	}
}

func TestVlogf(t *testing.T) {
	t.Parallel()
	t.Run("quiet emits nothing", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		vlogf(&buf, false, "hidden %d", 1)
		if buf.Len() != 0 {
			t.Errorf("vlogf quiet wrote %q, want nothing", buf.String())
		}
	})
	t.Run("verbose writes prefixed line", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		vlogf(&buf, true, "ref %q", "abc")
		got := buf.String()
		if !strings.HasPrefix(got, "turutan: ") || !strings.Contains(got, `"abc"`) {
			t.Errorf("vlogf verbose wrote %q, want turutan-prefixed line", got)
		}
	})
}
