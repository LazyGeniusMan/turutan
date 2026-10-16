// SPDX-License-Identifier: Apache-2.0

package scaffold

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const schemaFlowManifestV1 = "schema_version: 1\nengine_version: \"dev\"\nconflict: inline\n" +
	"inputs:\n" +
	"  project_name:\n    label: \"Project Name\"\n    type: string\n    default: demo\n" +
	"  color:\n    label: \"Color\"\n    type: string\n    default: red\n    options: [red, blue]\n"

const schemaFlowManifestV2 = "schema_version: 1\nengine_version: \"dev\"\nconflict: inline\n" +
	"inputs:\n" +
	"  project_name:\n    label: \"Project Name\"\n    type: string\n    default: demo\n" +
	"  color:\n    label: \"Color\"\n    type: string\n    default: red\n    options: [red, blue, green]\n" +
	"  size:\n    label: \"Size\"\n    type: string\n    default: m\n"

func readFlowFile(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name))) // #nosec G304 -- test-only path under t.TempDir() with fixed schema file names
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	return string(data)
}

func TestInitWritesVersionedSchemas(t *testing.T) {
	projectDir, _ := initRegistryProject(t, schemaFlowManifestV1, map[string]string{"app.txt": "v1\n"})
	for _, rel := range []string{
		".config/turutan/schemas/state.schema.json",
		".config/turutan/schemas/lock.schema.json",
		".config/turutan/schemas/state-data.schema.json",
	} {
		if _, err := os.Stat(filepath.Join(projectDir, filepath.FromSlash(rel))); err != nil {
			t.Errorf("init missed %s: %v", rel, err)
		}
	}
	stateRaw := readFlowFile(t, projectDir, ".config/turutan/state.yml")
	if !strings.Contains(stateRaw, "schema_version: 1") {
		t.Errorf("state.yml misses schema_version: 1:\n%s", stateRaw)
	}
	if !strings.Contains(stateRaw, "$schema: ./schemas/state.schema.json") {
		t.Errorf("state.yml misses its $schema reference:\n%s", stateRaw)
	}
	lockRaw := readFlowFile(t, projectDir, ".config/turutan/lock.json")
	if !strings.Contains(lockRaw, `"schema_version": 1`) {
		t.Errorf("lock.json misses schema_version 1:\n%s", lockRaw)
	}
	dataRaw := readFlowFile(t, projectDir, ".config/turutan/schemas/state-data.schema.json")
	if !strings.Contains(dataRaw, `"color"`) || !strings.Contains(dataRaw, `"blue"`) {
		t.Errorf("state-data schema misses the template inputs:\n%s", dataRaw)
	}
}

func TestUpdateRegeneratesStateDataSchema(t *testing.T) {
	projectDir, registryURI := initRegistryProject(t, schemaFlowManifestV1, map[string]string{"app.txt": "v1\n"})
	before := readFlowFile(t, projectDir, ".config/turutan/schemas/state-data.schema.json")
	writeTestRegistryFiles(t, registryURI, testRegistryTemplate, map[string]string{
		"template.yml": schemaFlowManifestV2,
	})
	releaseTestTemplate(t, testRegistryDir(t, registryURI), testRegistryTemplate)
	var stdout, stderr strings.Builder
	opts := updateTestOptions(&stdout, &stderr)
	opts.NonInteractive = true
	if _, err := Update(context.Background(), projectDir, opts); err != nil {
		t.Fatalf("Update error: %v", err)
	}
	after := readFlowFile(t, projectDir, ".config/turutan/schemas/state-data.schema.json")
	if before == after {
		t.Error("state-data schema unchanged after template inputs changed, want regeneration")
	}
	if !strings.Contains(after, `"size"`) || !strings.Contains(after, `"green"`) {
		t.Errorf("regenerated data schema misses new input/option:\n%s", after)
	}
	stateRaw := readFlowFile(t, projectDir, ".config/turutan/state.yml")
	if !strings.Contains(stateRaw, "schema_version: 1") {
		t.Errorf("updated state.yml misses schema_version: 1:\n%s", stateRaw)
	}
}

func TestStateSyncWritesStateDataSchema(t *testing.T) {
	projectDir, _ := initRegistryProject(t, schemaFlowManifestV1, map[string]string{"app.txt": "v1\n"})
	if err := os.Remove(filepath.Join(projectDir, ".config", "turutan", "schemas", "state-data.schema.json")); err != nil {
		t.Fatalf("removing data schema: %v", err)
	}
	var stdout, stderr strings.Builder
	if _, err := StateSync(context.Background(), projectDir, stateSyncTestOptions(&stdout, &stderr)); err != nil {
		t.Fatalf("StateSync error: %v", err)
	}
	dataRaw := readFlowFile(t, projectDir, ".config/turutan/schemas/state-data.schema.json")
	if !strings.Contains(dataRaw, `"color"`) {
		t.Errorf("synced data schema misses template inputs:\n%s", dataRaw)
	}
}
