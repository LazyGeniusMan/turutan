// SPDX-License-Identifier: Apache-2.0

package registry

import (
	"path/filepath"
	"strings"

	"github.com/LazyGeniusMan/turutan/internal/schemagen"
)

// SchemaVersion is the schema_version every registry-owned generated file
// carries. Loads accept only this version; saves always stamp it.
// ~keep
const SchemaVersion = 1

// Registry schema file names under the registry root and the index $schema
// reference. Versioned manifests reference their schema with a path relative
// to their own location.
// ~keep
const (
	IndexSchemaFileName    = ".config/turutan/schemas/registry-index.schema.json"
	ManifestSchemaFileName = ".config/turutan/schemas/template-manifest.schema.json"
	IndexSchemaRef         = ".config/turutan/schemas/registry-index.schema.json"
)

// IndexJSONSchema returns the reflection-generated JSON Schema for
// RegistryIndex, derived from the same struct the read/write logic uses.
// The templates array requires minItems 0 so fresh `[]` indexes validate.
// ~keep
func IndexJSONSchema() map[string]any {
	schema := schemagen.StructSchema("turutan registry index", RegistryIndex{}, "json")
	if properties, ok := schema["properties"].(map[string]any); ok {
		if templates, ok := properties["templates"].(map[string]any); ok {
			templates["minItems"] = 0
		}
	}
	return schema
}

// ManifestJSONSchema returns the reflection-generated JSON Schema for
// TemplateManifest, derived from the same struct the read/write logic uses.
// ~keep
func ManifestJSONSchema() map[string]any {
	return schemagen.StructSchema("turutan template manifest", TemplateManifest{}, "json")
}

// relativeSchemaRef returns the slash-separated path from the directory
// holding fromFile to toFile, prefixed with ./ when it would otherwise be
// bare, for use as a relative $schema reference.
// ~keep
func relativeSchemaRef(fromFile, toFile string) (string, error) {
	rel, err := filepath.Rel(filepath.Dir(fromFile), toFile)
	if err != nil {
		return "", err
	}
	slash := filepath.ToSlash(rel)
	if !strings.HasPrefix(slash, ".") {
		slash = "./" + slash
	}
	return slash, nil
}
