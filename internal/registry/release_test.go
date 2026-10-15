// SPDX-License-Identifier: Apache-2.0

package registry

import (
	"testing"
)

func unreleasedTestIndex(t *testing.T) *RegistryIndex {
	t.Helper()
	author, err := ParseAuthor("A <a@b.co>")
	if err != nil {
		t.Fatal(err)
	}
	return &RegistryIndex{
		SchemaVersion: SchemaVersion,
		Name:          "r", Version: "0.1.0", Author: author,
		Templates: []RegistryTemplate{{Name: "app", Author: author}},
	}
}

func TestNextReleaseVersionUnreleased(t *testing.T) {
	t.Parallel()
	for _, bump := range []ReleaseBump{ReleaseMajor, ReleaseMinor, ReleasePatch, ReleaseSame} {
		t.Run(string(bump)+" starts at 0.1.0", func(t *testing.T) {
			t.Parallel()
			next, existed, err := NextReleaseVersion(unreleasedTestIndex(t), "app", bump)
			if err != nil {
				t.Fatalf("NextReleaseVersion error: %v", err)
			}
			if next != "0.1.0" {
				t.Errorf("next = %q, want 0.1.0", next)
			}
			if existed {
				t.Errorf("existed = true, want false for a never-released entry")
			}
		})
	}
}

func TestNextReleaseVersionMissing(t *testing.T) {
	t.Parallel()
	next, existed, err := NextReleaseVersion(unreleasedTestIndex(t), "missing", ReleasePatch)
	if err != nil {
		t.Fatalf("NextReleaseVersion error: %v", err)
	}
	if next != "0.1.0" || existed {
		t.Errorf("next = %q existed = %v, want 0.1.0 false", next, existed)
	}
}

func TestBuildReleaseURIFromUnreleased(t *testing.T) {
	t.Parallel()
	index := unreleasedTestIndex(t)
	existing, err := index.FindTemplate("app")
	if err != nil {
		t.Fatal(err)
	}
	uri, err := BuildReleaseURI("fs", "app", "0.1.0", "", existing)
	if err != nil {
		t.Fatalf("BuildReleaseURI fs error: %v", err)
	}
	if uri.String() != "fs://"+ReleaseSnapshotPath("app", "0.1.0") {
		t.Errorf("uri = %q, want the versioned snapshot path", uri.String())
	}
	if _, err := BuildReleaseURI("oci", "app", "0.1.0", "", existing); err == nil {
		t.Errorf("oci first cut without override succeeded, want the first-release error")
	}
	const publishRepo = "oci://ghcr.io/example/r/templates/app"
	uri, err = BuildReleaseURI("oci", "app", "0.1.0", publishRepo, existing)
	if err != nil {
		t.Fatalf("BuildReleaseURI oci with override error: %v", err)
	}
	if uri.Scheme != SchemeOCI || uri.Ref != "0.1.0" {
		t.Errorf("uri = %q, want the publish repo at 0.1.0", uri.String())
	}
}

func TestUpsertIndexReleasePromotesUnreleased(t *testing.T) {
	t.Parallel()
	index := unreleasedTestIndex(t)
	author, err := ParseAuthor("A <a@b.co>")
	if err != nil {
		t.Fatal(err)
	}
	uri, err := ParseURI(ReleaseSnapshotPath("app", "0.1.0"))
	if err != nil {
		t.Fatal(err)
	}
	UpsertIndexRelease(index, TemplateManifest{Name: "app", Version: "0.1.0", Author: author, URI: uri})
	found, err := index.FindTemplate("app")
	if err != nil {
		t.Fatal(err)
	}
	if !found.IsReleased() {
		t.Fatalf("IsReleased = false after the first UpsertIndexRelease")
	}
	if found.Version != "0.1.0" || len(found.Releases) != 1 || found.Releases[0] != "0.1.0" {
		t.Errorf("entry = %+v, want version 0.1.0 with one release", found)
	}
	if found.URI == nil || found.URI.String() != "fs://"+ReleaseSnapshotPath("app", "0.1.0") {
		t.Errorf("uri = %+v, want the versioned snapshot path", found.URI)
	}
	if err := index.Validate(); err != nil {
		t.Errorf("Validate error after promotion: %v", err)
	}
}
