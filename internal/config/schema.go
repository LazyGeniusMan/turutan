// SPDX-License-Identifier: Apache-2.0

package config

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/LazyGeniusMan/turutan/internal/schemagen"
)

// Schema file names under the project root and the relative $schema
// references the generated state and lock files carry.
// ~keep
const (
	StateSchemaFileName     = ".config/turutan/schemas/state.schema.json"
	LockSchemaFileName      = ".config/turutan/schemas/lock.schema.json"
	StateDataSchemaFileName = ".config/turutan/schemas/state-data.schema.json"
	TemplateSchemaFileName  = ".config/turutan/schemas/template.schema.json"

	StateSchemaRef = "./schemas/state.schema.json"
	LockSchemaRef  = "./schemas/lock.schema.json"
	StateDataRef   = "state-data.schema.json"
)

// StateJSONSchema returns the reflection-generated JSON Schema for State,
// derived from the same struct the read/write logic uses. The free-form
// data property defers to the per-template state-data schema.
// ~keep
func StateJSONSchema() map[string]any {
	schema := schemagen.StructSchema("turutan state", State{}, "yaml")
	if properties, ok := schema["properties"].(map[string]any); ok {
		properties["data"] = map[string]any{"$ref": StateDataRef}
	}
	return schema
}

// LockJSONSchema returns the reflection-generated JSON Schema for Lock,
// derived from the same struct the read/write logic uses.
// ~keep
func LockJSONSchema() map[string]any {
	return schemagen.StructSchema("turutan lock", Lock{}, "json")
}

// ManifestJSONSchema returns the reflection-generated JSON Schema for the
// template authoring manifest (template.yml), derived from the same struct
// the read/write logic uses. Adding a Manifest field changes the schema
// with no manual edit.
// ~keep
func ManifestJSONSchema() map[string]any {
	return schemagen.StructSchema("turutan template", Manifest{}, "yaml")
}

// TemplateSchemaRefFor returns the slash-separated relative $schema path
// from a template.yml file to the registry-root template schema file.
// ~keep
func TemplateSchemaRefFor(templateYMLPath, schemaPath string) (string, error) {
	rel, err := filepath.Rel(filepath.Dir(templateYMLPath), schemaPath)
	if err != nil {
		return "", err
	}
	slash := filepath.ToSlash(rel)
	if len(slash) > 0 && slash[0] != '.' {
		slash = "./" + slash
	}
	return slash, nil
}

// pinned template manifest inputs: type maps to a JSON type, options to an
// enum (element enum for arrays), default to a default, and inputs without
// a default or condition to required keys.
// ~keep
func StateDataJSONSchema(manifest *Manifest) map[string]any {
	properties := map[string]any{}
	var required []string
	if manifest != nil {
		keys := make([]string, 0, len(manifest.Inputs))
		for key := range manifest.Inputs {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			input := manifest.Inputs[key]
			properties[key] = manifestInputSchema(input)
			if input.Default == nil && strings.TrimSpace(input.If) == "" {
				required = append(required, key)
			}
		}
	}
	schema := map[string]any{
		"$schema":    schemagen.Draft,
		"title":      "turutan state data",
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func manifestInputSchema(input ManifestInput) map[string]any {
	var schema map[string]any
	switch input.Type {
	case "bool":
		schema = map[string]any{"type": "boolean"}
	case "array":
		items := map[string]any{"type": "string"}
		if len(input.Options) > 0 {
			items["enum"] = append([]string(nil), input.Options...)
		}
		schema = map[string]any{"type": "array", "items": items}
	default:
		schema = map[string]any{"type": "string"}
		if len(input.Options) > 0 {
			schema["enum"] = append([]string(nil), input.Options...)
		}
	}
	if strings.TrimSpace(input.Label) != "" {
		schema["title"] = input.Label
	}
	if strings.TrimSpace(input.Help) != "" {
		schema["description"] = input.Help
	}
	if input.Default != nil {
		schema["default"] = input.Default
	}
	return schema
}

// SaveStateDataSchema regenerates the per-template state-data schema from
// the pinned manifest inputs. Callers rewrite it on every state write so
// template input changes surface on the next init, update or sync.
// ~keep
func SaveStateDataSchema(dir string, manifest *Manifest) error {
	path := filepath.Join(dir, filepath.FromSlash(StateDataSchemaFileName))
	if err := schemagen.WriteFile(path, StateDataJSONSchema(manifest)); err != nil {
		return err
	}
	return nil
}

func writeGeneratedSchema(dir, name string, schema map[string]any) error {
	path := filepath.Join(dir, filepath.FromSlash(name))
	return schemagen.WriteFile(path, schema)
}
