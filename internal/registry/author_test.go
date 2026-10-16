// SPDX-License-Identifier: Apache-2.0

package registry

import (
	"errors"
	"testing"
)

func TestParseAuthor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		input       string
		expected    Author
		expectedStr string
		wantErr     bool
	}{
		{
			name:        "name only",
			input:       "Jane Doe",
			expected:    Author{Name: "Jane Doe"},
			expectedStr: "Jane Doe",
		},
		{
			name:        "name and email",
			input:       "Jane Doe <jane@example.com>",
			expected:    Author{Name: "Jane Doe", Email: "jane@example.com"},
			expectedStr: "Jane Doe <jane@example.com>",
		},
		{
			name:  "full form",
			input: "Jane Doe <jane@example.com> (https://example.com)",
			expected: Author{
				Name:    "Jane Doe",
				Email:   "jane@example.com",
				Website: "https://example.com",
			},
			expectedStr: "Jane Doe <jane@example.com> (https://example.com)",
		},
		{
			name:        "name and website",
			input:       "Jane Doe (https://example.com)",
			expected:    Author{Name: "Jane Doe", Website: "https://example.com"},
			expectedStr: "Jane Doe (https://example.com)",
		},
		{
			name:    "empty",
			input:   "  ",
			wantErr: true,
		},
		{
			name:    "bad email",
			input:   "Jane <not-an-email>",
			wantErr: true,
		},
		{
			name:    "non http website",
			input:   "Jane (ftp://example.com)",
			wantErr: true,
		},
		{
			name:    "name with brackets",
			input:   "Jane <Doe>",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseAuthor(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseAuthor(%q) = %+v, want error", tt.input, got)
				}
				if !errors.Is(err, ErrInvalidAuthor) {
					t.Errorf("ParseAuthor(%q) error = %v, want ErrInvalidAuthor", tt.input, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseAuthor(%q) error: %v", tt.input, err)
			}
			if got != tt.expected {
				t.Errorf("ParseAuthor(%q) = %+v, want %+v", tt.input, got, tt.expected)
			}
			if got.String() != tt.expectedStr {
				t.Errorf("String() = %q, want %q", got.String(), tt.expectedStr)
			}
		})
	}
}

func TestAuthorRoundTrip(t *testing.T) {
	t.Parallel()
	raw := "Jane Doe <jane@example.com> (https://example.com)"
	parsed, err := ParseAuthor(raw)
	if err != nil {
		t.Fatalf("ParseAuthor error: %v", err)
	}
	reparsed, err := ParseAuthor(parsed.String())
	if err != nil {
		t.Fatalf("reparse error: %v", err)
	}
	if reparsed != parsed {
		t.Errorf("round trip = %+v, want %+v", reparsed, parsed)
	}
}
