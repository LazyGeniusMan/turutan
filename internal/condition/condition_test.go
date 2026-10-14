// SPDX-License-Identifier: Apache-2.0

package condition

import (
	"strings"
	"testing"
)

func TestParseConditionalBool(t *testing.T) {
	t.Parallel()
	truthy := []string{"true", "True", "TRUE", "1", "yes", "Yes", "YES"}
	for _, s := range truthy {
		if got, err := ParseConditionalBool(s); err != nil || !got {
			t.Errorf("ParseConditionalBool(%q) = %v, %v; want true, nil", s, got, err)
		}
	}
	falsy := []string{"false", "False", "0", "", "no", "No"}
	for _, s := range falsy {
		if got, err := ParseConditionalBool(s); err != nil || got {
			t.Errorf("ParseConditionalBool(%q) = %v, %v; want false, nil", s, got, err)
		}
	}
	if _, err := ParseConditionalBool("maybe"); err == nil ||
		!strings.Contains(err.Error(), "not a boolean") {
		t.Errorf("ParseConditionalBool(maybe) error = %v, want not-a-boolean", err)
	}
}

func TestEvaluatePredicate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		ifExpr  string
		view    map[string]any
		want    bool
		wantErr string
		wantKey string
	}{
		{name: "empty asks", ifExpr: "", view: map[string]any{}, want: true},
		{name: "blank asks", ifExpr: "   ", view: map[string]any{}, want: true},
		{
			name:   "ai present asks",
			ifExpr: `{{contains .profiles "ai"}}`,
			view:   map[string]any{"profiles": []string{"core", "ai", "custom"}},
			want:   true,
		},
		{
			name:   "ai absent skips",
			ifExpr: `{{contains .profiles "ai"}}`,
			view:   map[string]any{"profiles": []string{"core", "go", "custom"}},
			want:   false,
		},
		{
			name:   "any slice profiles",
			ifExpr: `{{contains .profiles "ai"}}`,
			view:   map[string]any{"profiles": []any{"core", "ai"}},
			want:   true,
		},
		{
			name:   "other data eq true",
			ifExpr: `{{eq .project_name "x"}}`,
			view:   map[string]any{"project_name": "x", "profiles": []string{"core"}},
			want:   true,
		},
		{
			name:   "other data eq false",
			ifExpr: `{{eq .project_name "x"}}`,
			view:   map[string]any{"project_name": "y", "profiles": []string{"core"}},
			want:   false,
		},
		{
			name:    "missing key retries",
			ifExpr:  `{{eq .project_name "x"}}`,
			view:    map[string]any{"profiles": []string{"core"}},
			wantErr: `no entry for key "project_name"`,
			wantKey: "project_name",
		},
		{
			name:    "missing profiles retries",
			ifExpr:  `{{contains .profiles "ai"}}`,
			view:    map[string]any{},
			wantErr: `no entry for key "profiles"`,
			wantKey: "profiles",
		},
		{
			name:    "non-bool errors",
			ifExpr:  `{{.project_name}}`,
			view:    map[string]any{"project_name": "demo"},
			wantErr: "not a boolean",
		},
		{
			name:    "parse error",
			ifExpr:  `{{contains .profiles}`,
			view:    map[string]any{"profiles": []string{"ai"}},
			wantErr: "parsing",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := EvaluatePredicate(tt.ifExpr, tt.view)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("EvaluatePredicate error = %v, want containing %q", err, tt.wantErr)
				}
				if tt.wantKey != "" {
					missing, ok := err.(*MissingKeyError)
					if !ok {
						t.Fatalf("error type = %T, want *MissingKeyError", err)
					}
					if missing.Key != tt.wantKey {
						t.Errorf("missing key = %q, want %q", missing.Key, tt.wantKey)
					}
					if ParseMissingKey(err) != tt.wantKey {
						t.Errorf("ParseMissingKey = %q, want %q", ParseMissingKey(err), tt.wantKey)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("EvaluatePredicate error = %v", err)
			}
			if got != tt.want {
				t.Errorf("EvaluatePredicate = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTemplateFuncMap(t *testing.T) {
	t.Parallel()
	funcs := TemplateFuncMap()
	if _, ok := funcs["contains"]; !ok {
		t.Error("func map lacks contains override")
	}
	if _, ok := funcs["env"]; ok {
		t.Error("func map exposes env, want it removed")
	}
	if _, ok := funcs["expandenv"]; ok {
		t.Error("func map exposes expandenv, want it removed")
	}
}
