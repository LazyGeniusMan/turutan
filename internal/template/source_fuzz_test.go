// SPDX-License-Identifier: Apache-2.0

package template

import (
	"strings"
	"testing"
)

var fuzzSeedCorpus = []string{
	"git::https://github.com/org/web.git//react?ref=v1.2.0",
	"git::https://github.com/org/web.git@main",
	"git@github.com:org/web.git//web/react?ref=main",
	"git@github.com:org/web.git@main",
	"git::git@github.com:org/web.git@main",
	"https://github.com/org/mono.git//services/api?ref=^2.1",
	"./local",
	"../tpl",
	"/abs/path",
	"file:///abs/path",
	"./mono//services/api",
	"default",
	"",
	"git::",
	"git::https://github.com/org/web.git//?ref=main",
	"https://github.com/org/web.git//sub?ref=v1&depth=5",
	"https://github.com/org/web.git?depth=0",
	"https://github.com/org/web.git?depth=abc",
	"https://github.com/org/web.git?bogus=1",
	"git::https://github.com/org/web.git//a/../../b",
	"ssh://git@github.com/org/web.git//sub?ref=main",
	"git://github.com/org/web.git",
	"ftp://example.com/org/web.git",
	"https://" + "user" + ":" + "redacted" + "@github.com/org/web.git//sub?ref=main",
	"user@host.xz:path/to/repo.git",
	"./x?ref=v1&depth=2",
}

func FuzzParseSource(f *testing.F) {
	for _, seed := range fuzzSeedCorpus {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		src, err := ParseSource(raw)
		if err != nil {
			return
		}
		if src.Depth <= 0 {
			t.Errorf("ParseSource(%q): Depth = %d, want positive", raw, src.Depth)
		}
		switch src.Kind {
		case KindRemoteGit, KindLocalGit, KindFilesystem:
		default:
			t.Errorf("ParseSource(%q): unknown Kind %q", raw, src.Kind)
		}
		if src.Subpath != "" {
			if strings.HasPrefix(src.Subpath, "/") {
				t.Errorf("ParseSource(%q): absolute Subpath %q", raw, src.Subpath)
			}
			for segment := range strings.SplitSeq(src.Subpath, "/") {
				if segment == ".." {
					t.Errorf("ParseSource(%q): Subpath %q escapes", raw, src.Subpath)
				}
			}
		}
		if raw == DefaultAlias {
			if src.Raw != DefaultURL+"?ref="+DefaultRef {
				t.Errorf("ParseSource(%q): Raw = %q, want the default URL", raw, src.Raw)
			}
		} else if src.Raw != raw {
			t.Errorf("ParseSource(%q): Raw = %q, want the input back", raw, src.Raw)
		}
		canonical := src.String()
		if canonical == "" {
			t.Errorf("ParseSource(%q): empty canonical form", raw)
		}
		if src.Kind == KindRemoteGit {
			reparsed, err := ParseSource(canonical)
			if err != nil {
				t.Errorf("ParseSource(%q): round-trip of %q failed: %v", raw, canonical, err)
				return
			}
			if reparsed.Repo != src.Repo || reparsed.Subpath != src.Subpath ||
				reparsed.RequestedRef != src.RequestedRef || reparsed.Depth != src.Depth {
				t.Errorf("ParseSource(%q): round-trip mismatch: %+v vs %+v", raw, src, reparsed)
			}
		}
	})
}
