// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"testing"
)

func TestCheck(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		stored  string
		current string
		wantErr bool
	}{
		{name: "equal passes", stored: "0.1.0", current: "0.1.0", wantErr: false},
		{name: "newer current passes and bumps", stored: "0.1.0", current: "0.2.0", wantErr: false},
		{name: "older current errors", stored: "0.2.0", current: "0.1.0", wantErr: true},
		{name: "empty stored skips", stored: "", current: "0.1.0", wantErr: false},
		{name: "empty current skips", stored: "0.1.0", current: "", wantErr: false},
		{name: "dev stored skips", stored: "dev", current: "0.1.0", wantErr: false},
		{name: "dev current skips", stored: "0.1.0", current: "dev", wantErr: false},
		{name: "turutan prefix normalized", stored: "turutan/0.1.0", current: "v0.2.0", wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := Check(tt.stored, tt.current)
			if (err != nil) != tt.wantErr {
				t.Errorf("Check(%q, %q) error = %v, wantErr %v", tt.stored, tt.current, err, tt.wantErr)
			}
		})
	}
}

func TestBump(t *testing.T) {
	t.Parallel()
	got, err := Bump("0.1.0", "0.2.0")
	if err != nil {
		t.Fatalf("Bump error: %v", err)
	}
	if got != "0.2.0" {
		t.Errorf("Bump = %q, want 0.2.0", got)
	}
	if _, err := Bump("0.2.0", "0.1.0"); err == nil {
		t.Error("Bump older succeeded, want error")
	}
}

func TestValidateField(t *testing.T) {
	t.Parallel()
	for _, ok := range []string{"0.1.0", "dev", "turutan/1.2.3", "v2.0.0"} {
		if err := ValidateField(ok); err != nil {
			t.Errorf("ValidateField(%q) error: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "sometime", ">=1.0"} {
		if err := ValidateField(bad); err == nil {
			t.Errorf("ValidateField(%q) succeeded, want error", bad)
		}
	}
}
