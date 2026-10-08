// SPDX-License-Identifier: Apache-2.0

// Package git wraps pure-Go git transport (ls-remote, clone, open).
// It is the only package that may import go-git; callers use the plain
// Ref/CloneOptions surface below so no go-git types leak out.
package git

import (
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/storage/memory"
)

// ShoreDepth is the default shallow fetch depth (spec §3: depth defaults to 1).
const ShallowDepth = 1

// maxShortHashLen bounds short-SHA prefix matching so absurd inputs fail fast.
const maxShortHashLen = 64

// CloneOptions describes a fetch; transport arrives in M1.
type CloneOptions struct {
	URL   string
	Ref   string
	Depth int
	Dir   string
}

// Ref is one advertised remote reference: Name is the full ref name
// (for example "refs/heads/main" or "HEAD"), Hash its object hash, and
// Target the symref target for symbolic references such as HEAD.
type Ref struct {
	Name   string
	Hash   string
	Target string
}

// redacted strips credentials from a URL for error messages and logs:
// tokens and passwords must never be echoed (secrets-handling rule).
// scp-like user@host:path keeps its username (an identity, not a secret).
func redacted(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User == nil {
		return raw
	}
	parsed.User = nil
	return parsed.String()
}

// ListRefs runs the ls-remote equivalent against url and returns every
// advertised reference. It performs no checkout and writes nothing to disk.
func ListRefs(url string) ([]Ref, error) {
	remote := git.NewRemote(memory.NewStorage(), &config.RemoteConfig{
		Name: "origin",
		URLs: []string{url},
	})
	refs, err := remote.List(&git.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing refs of %q: %w", redacted(url), err)
	}
	out := make([]Ref, 0, len(refs))
	for _, ref := range refs {
		entry := Ref{Name: ref.Name().String(), Hash: ref.Hash().String()}
		if ref.Type() == plumbing.SymbolicReference {
			entry.Target = ref.Target().String()
		}
		out = append(out, entry)
	}
	return out, nil
}

// ResolveRef maps a user ref expression to a commit hash using a prior
// ls-remote advertisement: empty expression selects the default branch
// (HEAD symref, else main, else master); full or short SHAs match by
// prefix; branches and tags match exactly; anything else is tried as a
// Masterminds/semver constraint over the tag list, highest version wins.
func ResolveRef(url, expr string, advertised []Ref) (string, error) {
	branches := map[string]string{}
	tags := map[string]string{}
	hashes := map[string]string{}
	headTarget := ""
	for _, ref := range advertised {
		hashes[strings.ToLower(ref.Hash)] = ref.Hash
		switch {
		case ref.Name == "HEAD":
			headTarget = ref.Target
		case strings.HasPrefix(ref.Name, "refs/heads/"):
			branches[strings.TrimPrefix(ref.Name, "refs/heads/")] = ref.Hash
		case strings.HasPrefix(ref.Name, "refs/tags/"):
			name := strings.TrimPrefix(ref.Name, "refs/tags/")
			name = strings.TrimSuffix(name, "^{}")
			if prev, ok := tags[name]; !ok || strings.HasSuffix(ref.Name, "^{}") || prev == "" {
				tags[name] = ref.Hash
			}
		}
	}
	if expr == "" {
		if headTarget != "" {
			if h, ok := branches[strings.TrimPrefix(headTarget, "refs/heads/")]; ok {
				return h, nil
			}
		}
		if h, ok := branches["main"]; ok {
			return h, nil
		}
		if h, ok := branches["master"]; ok {
			return h, nil
		}
		return "", fmt.Errorf("resolving default branch of %q: no HEAD, main or master ref", redacted(url))
	}
	if hash, ok := matchHashPrefix(hashes, expr); ok {
		return hash, nil
	}
	if h, ok := branches[expr]; ok {
		return h, nil
	}
	if h, ok := tags[expr]; ok {
		return h, nil
	}
	hash, err := resolveSemver(tags, expr)
	if err != nil {
		return "", fmt.Errorf("resolving ref %q of %q: %w", expr, redacted(url), err)
	}
	return hash, nil
}

// ResolveRemoteRef lists url then resolves expr in one step.
func ResolveRemoteRef(url, expr string) (string, error) {
	advertised, err := ListRefs(url)
	if err != nil {
		return "", err
	}
	return ResolveRef(url, expr, advertised)
}

// Clone shallow-clones url into dir (creating it) and checks out ref when
// non-empty, returning the checked-out commit hash. A non-positive depth
// selects ShallowDepth. The clone fetches all branch tips, so branches and
// tags resolve locally; a SHA outside the shallow boundary fails with a
// clear error instead of silently checking out the default branch.
func Clone(url, ref, dir string, depth int) (string, error) {
	if depth <= 0 {
		depth = ShallowDepth
	}
	repo, err := git.PlainClone(dir, &git.CloneOptions{URL: url, Depth: depth})
	if err != nil {
		return "", fmt.Errorf("cloning %q: %w", redacted(url), err)
	}
	if ref == "" {
		return headHash(repo)
	}
	hash, err := repo.ResolveRevision(plumbing.Revision(ref))
	if err != nil {
		return "", fmt.Errorf("resolving ref %q in %q: %w", ref, redacted(url), err)
	}
	worktree, err := repo.Worktree()
	if err != nil {
		return "", fmt.Errorf("opening worktree of %q: %w", redacted(url), err)
	}
	if err := worktree.Checkout(&git.CheckoutOptions{Hash: *hash}); err != nil {
		return "", fmt.Errorf("checking out ref %q in %q: %w", ref, redacted(url), err)
	}
	return hash.String(), nil
}

// CloneWithOptions clones per opts, defaulting empty Depth to ShallowDepth.
func CloneWithOptions(opts CloneOptions) (string, error) {
	return Clone(opts.URL, opts.Ref, opts.Dir, opts.Depth)
}

// IsGitRepo reports whether path is a git repository (bare-aware) by
// probing it with PlainOpen. It never mutates path.
func IsGitRepo(path string) bool {
	_, err := git.PlainOpen(path)
	return err == nil
}

// ResolveLocal resolves expr inside the repository at path without
// mutating it: empty expression reads HEAD (falling back to main, then
// master for bare repositories with an unborn HEAD); otherwise it uses
// ResolveRevision, so branches, tags and SHAs all work offline.
func ResolveLocal(path, expr string) (string, error) {
	repo, err := git.PlainOpen(path)
	if err != nil {
		return "", fmt.Errorf("opening git repository %q: %w", path, err)
	}
	if expr == "" {
		if hash, err := headHash(repo); err == nil {
			return hash, nil
		}
		for _, branch := range []string{"main", "master"} {
			if hash, err := repo.ResolveRevision(plumbing.Revision("refs/heads/" + branch)); err == nil {
				return hash.String(), nil
			}
		}
		return "", fmt.Errorf("resolving HEAD of %q: no HEAD, main or master ref", path)
	}
	hash, err := repo.ResolveRevision(plumbing.Revision(expr))
	if err != nil {
		return "", fmt.Errorf("resolving ref %q in %q: %w", expr, path, err)
	}
	return hash.String(), nil
}

// Checkout moves the repository at dir to sha in detached HEAD mode.
// dir must hold a previous clone; sha typically comes from ResolveRef or
// ResolveLocal so semver expressions are resolved before calling.
func Checkout(dir, sha string) error {
	repo, err := git.PlainOpen(dir)
	if err != nil {
		return fmt.Errorf("opening git repository %q: %w", dir, err)
	}
	hash, err := repo.ResolveRevision(plumbing.Revision(sha))
	if err != nil {
		return fmt.Errorf("resolving commit %q in %q: %w", sha, dir, err)
	}
	worktree, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("opening worktree of %q: %w", dir, err)
	}
	if err := worktree.Checkout(&git.CheckoutOptions{Hash: *hash}); err != nil {
		return fmt.Errorf("checking out commit %q in %q: %w", sha, dir, err)
	}
	return nil
}

// headHash returns the current HEAD commit hash of repo.
func headHash(repo *git.Repository) (string, error) {
	head, err := repo.Head()
	if err != nil {
		return "", fmt.Errorf("reading HEAD: %w", err)
	}
	return head.Hash().String(), nil
}

// matchHashPrefix matches expr as a full or abbreviated commit hash
// against advertised hashes; ambiguous short prefixes fail explicitly.
func matchHashPrefix(hashes map[string]string, expr string) (string, bool) {
	lowered := strings.ToLower(expr)
	if len(lowered) < 7 || len(lowered) > maxShortHashLen {
		return "", false
	}
	for i := 0; i < len(lowered); i++ {
		if !strings.ContainsRune("0123456789abcdef", rune(lowered[i])) {
			return "", false
		}
	}
	var match string
	ambiguous := false
	for hash := range hashes {
		if strings.HasPrefix(hash, lowered) {
			if match != "" {
				ambiguous = true
				break
			}
			match = hashes[hash]
		}
	}
	if ambiguous || match == "" {
		return "", false
	}
	return match, true
}

// resolveSemver picks the highest tag satisfying constraint expr.
func resolveSemver(tags map[string]string, expr string) (string, error) {
	constraint, err := semver.NewConstraint(expr)
	if err != nil {
		return "", fmt.Errorf("invalid ref %q: no branch, tag, SHA or semver constraint matches", expr)
	}
	type candidate struct {
		version *semver.Version
		hash    string
	}
	var candidates []candidate
	for name, hash := range tags {
		version, err := semver.NewVersion(strings.TrimPrefix(name, "v"))
		if err != nil {
			continue
		}
		if constraint.Check(version) {
			candidates = append(candidates, candidate{version: version, hash: hash})
		}
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("invalid ref %q: no tag satisfies the constraint", expr)
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].version.LessThan(candidates[j].version)
	})
	return candidates[len(candidates)-1].hash, nil
}
