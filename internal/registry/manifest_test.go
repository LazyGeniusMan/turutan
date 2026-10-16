// SPDX-License-Identifier: Apache-2.0

package registry

import (
	"errors"
	"strings"
	"testing"
)

func validManifest() *TemplateManifest {
	author, _ := ParseAuthor("Jane Doe <jane@example.com> (https://example.com)")
	uri, _ := ParseURI("oci://ghcr.io/lazygeniusman/turutan/templates/default:1.2.0")
	return &TemplateManifest{
		SchemaVersion: SchemaVersion,
		Name:          "default",
		Version:       "1.2.0",
		Author:        author,
		URI:           uri,
		Digest:        "sha256:" + strings.Repeat("a", 64),
		EngineVersion: "0.2.0",
		ManifestHash:  "sha256:" + strings.Repeat("b", 64),
		License:       "MIT-0",
	}
}

func TestTemplateManifestValidate(t *testing.T) {
	t.Parallel()
	t.Run("valid", func(t *testing.T) {
		t.Parallel()
		if err := validManifest().Validate(); err != nil {
			t.Errorf("Validate error: %v", err)
		}
	})
	tests := []struct {
		name   string
		mutate func(*TemplateManifest)
	}{
		{name: "empty name", mutate: func(m *TemplateManifest) { m.Name = "" }},
		{name: "bad name", mutate: func(m *TemplateManifest) { m.Name = "Bad Name!" }},
		{name: "bad version", mutate: func(m *TemplateManifest) { m.Version = ">=1.0" }},
		{
			name:   "bad author",
			mutate: func(m *TemplateManifest) { m.Author = Author{} },
		},
		{
			name:   "bad contributor",
			mutate: func(m *TemplateManifest) { m.Contributors = []Author{{Name: ""}} },
		},
		{
			name:   "bad uri",
			mutate: func(m *TemplateManifest) { m.URI = URI{} },
		},
		{
			name: "oci uri without tag",
			mutate: func(m *TemplateManifest) {
				uri, _ := ParseURI("oci://ghcr.io/org/repo")
				m.URI = uri
			},
		},
		{
			name:   "bad digest",
			mutate: func(m *TemplateManifest) { m.Digest = "md5:abc" },
		},
		{
			name:   "bad min engine",
			mutate: func(m *TemplateManifest) { m.EngineVersion = "sometime" },
		},
		{
			name:   "empty engine rejected",
			mutate: func(m *TemplateManifest) { m.EngineVersion = "" },
		},
		{
			name:   "bad manifest hash",
			mutate: func(m *TemplateManifest) { m.ManifestHash = "abc" },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			manifest := validManifest()
			tt.mutate(manifest)
			err := manifest.Validate()
			if err == nil {
				t.Fatalf("Validate succeeded, want error")
			}
			if !errors.Is(err, ErrInvalidManifest) {
				t.Errorf("Validate error = %v, want ErrInvalidManifest", err)
			}
		})
	}
	t.Run("nil manifest", func(t *testing.T) {
		t.Parallel()
		var manifest *TemplateManifest
		if err := manifest.Validate(); !errors.Is(err, ErrInvalidManifest) {
			t.Errorf("Validate error = %v, want ErrInvalidManifest", err)
		}
	})
}
