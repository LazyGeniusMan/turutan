// SPDX-License-Identifier: Apache-2.0

package registry

import (
	"errors"
	"slices"
	"testing"
)

func TestParseStrict(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "plain", input: "1.2.3"},
		{name: "prerelease", input: "1.2.3-rc.1"},
		{name: "missing patch", input: "1.2", wantErr: true},
		{name: "range", input: ">=1.2.3", wantErr: true},
		{name: "constraint", input: "^1.2", wantErr: true},
		{name: "empty", input: "", wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseStrict(tt.input)
			if tt.wantErr && err == nil {
				t.Fatalf("ParseStrict(%q) succeeded, want error", tt.input)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ParseStrict(%q) error: %v", tt.input, err)
			}
			if err != nil && !errors.Is(err, ErrInvalidVersion) {
				t.Errorf("ParseStrict(%q) error = %v, want ErrInvalidVersion", tt.input, err)
			}
		})
	}
}

func TestCompare(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		first    string
		second   string
		expected int
		wantErr  bool
	}{
		{name: "less", first: "1.2.3", second: "1.2.4", expected: -1},
		{name: "equal", first: "1.2.3", second: "1.2.3", expected: 0},
		{name: "greater", first: "2.0.0", second: "1.9.9", expected: 1},
		{name: "invalid", first: "nope", second: "1.0.0", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := Compare(tt.first, tt.second)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Compare succeeded, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Compare error: %v", err)
			}
			if got != tt.expected {
				t.Errorf("Compare(%q, %q) = %d, want %d", tt.first, tt.second, got, tt.expected)
			}
		})
	}
}

func TestSortDesc(t *testing.T) {
	t.Parallel()
	got, err := SortDesc([]string{"1.0.0", "2.0.0", "1.10.0", "1.2.3-rc.1"})
	if err != nil {
		t.Fatalf("SortDesc error: %v", err)
	}
	expected := []string{"2.0.0", "1.10.0", "1.2.3-rc.1", "1.0.0"}
	if !slices.Equal(got, expected) {
		t.Errorf("SortDesc = %v, want %v", got, expected)
	}
	t.Run("invalid entry", func(t *testing.T) {
		t.Parallel()
		if _, err := SortDesc([]string{"1.0.0", "nope"}); err == nil {
			t.Errorf("SortDesc succeeded, want error")
		}
	})
}

func TestBump(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		fn       func(string) (string, error)
		input    string
		expected string
		wantErr  bool
	}{
		{name: "major", fn: BumpMajor, input: "1.2.3", expected: "2.0.0"},
		{name: "minor", fn: BumpMinor, input: "1.2.3", expected: "1.3.0"},
		{name: "patch", fn: BumpPatch, input: "1.2.3", expected: "1.2.4"},
		{name: "patch drops prerelease", fn: BumpPatch, input: "1.2.3-rc.1", expected: "1.2.4"},
		{name: "invalid", fn: BumpMajor, input: "nope", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tt.fn(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("bump(%q) succeeded, want error", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("bump error: %v", err)
			}
			if got != tt.expected {
				t.Errorf("bump(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
