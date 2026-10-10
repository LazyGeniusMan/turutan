// SPDX-License-Identifier: Apache-2.0

package git

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/Masterminds/semver/v3"
	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/storage/memory"
)

const ShallowDepth = 1

const (
	minShortHashLen = 7
	maxShortHashLen = 64
)

type CloneOptions struct {
	URL   string
	Ref   string
	Depth int
	Dir   string
}

type Ref struct {
	Name   string
	Hash   string
	Target string
}

func redacted(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User == nil {
		return raw
	}
	parsed.User = nil
	return parsed.String()
}

func ListRefs(ctx context.Context, url string) ([]Ref, error) {
	clientOpts, insecure, err := ClientOptions(url)
	if err != nil {
		return nil, err
	}
	if insecure {
		warnInsecure()
	}
	remote := git.NewRemote(memory.NewStorage(), &config.RemoteConfig{
		Name: "origin",
		URLs: []string{url},
	})
	refs, err := remote.ListContext(
		ctx, &git.ListOptions{ClientOptions: clientOpts})
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

func indexRefs(
	advertised []Ref,
) (branches, tags, hashes map[string]string, headTarget string) {
	branches = map[string]string{}
	tags = map[string]string{}
	hashes = map[string]string{}
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
			if prev, ok := tags[name]; !ok ||
				strings.HasSuffix(ref.Name, "^{}") || prev == "" {
				tags[name] = ref.Hash
			}
		}
	}
	return branches, tags, hashes, headTarget
}

func defaultBranch(
	url string,
	branches map[string]string,
	headTarget string,
) (string, error) {
	if headTarget != "" {
		if h, ok := branches[strings.TrimPrefix(
			headTarget, "refs/heads/")]; ok {
			return h, nil
		}
	}
	if h, ok := branches["main"]; ok {
		return h, nil
	}
	if h, ok := branches["master"]; ok {
		return h, nil
	}
	return "", fmt.Errorf(
		"resolving default branch of %q: no HEAD, main or master ref",
		redacted(url))
}

func ResolveRef(url, expr string, advertised []Ref) (string, error) {
	branches, tags, hashes, headTarget := indexRefs(advertised)
	if expr == "" {
		return defaultBranch(url, branches, headTarget)
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

func ResolveRemoteRef(
	ctx context.Context,
	url, expr string,
) (string, error) {
	advertised, err := ListRefs(ctx, url)
	if err != nil {
		return "", err
	}
	return ResolveRef(url, expr, advertised)
}

func Clone(
	ctx context.Context,
	url, ref, dir string,
	depth int,
) (string, error) {
	if depth <= 0 {
		depth = ShallowDepth
	}
	clientOpts, insecure, err := ClientOptions(url)
	if err != nil {
		return "", err
	}
	if insecure {
		warnInsecure()
	}
	repo, err := git.PlainCloneContext(ctx,
		dir, &git.CloneOptions{
			URL: url, Depth: depth, ClientOptions: clientOpts,
		})
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

func CloneWithOptions(
	ctx context.Context,
	opts CloneOptions,
) (string, error) {
	return Clone(ctx, opts.URL, opts.Ref, opts.Dir, opts.Depth)
}

func IsBare(path string) (bool, error) {
	repo, err := git.PlainOpen(path)
	if err != nil {
		return false, fmt.Errorf("opening git repository %q: %w", path, err)
	}
	return isBareRepo(repo), nil
}

func isBareRepo(repo *git.Repository) bool {
	_, err := repo.Worktree()
	return err != nil
}

func IsGitRepo(path string) bool {
	_, err := git.PlainOpen(path)
	return err == nil
}

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
		if isBareRepo(repo) {
			return "", fmt.Errorf("resolving HEAD of bare repository %q: no HEAD, main or master ref", path)
		}
		return "", fmt.Errorf("resolving HEAD of %q: no HEAD, main or master ref", path)
	}
	if hash, err := repo.ResolveRevision(plumbing.Revision(expr)); err == nil {
		return hash.String(), nil
	}
	tags, err := listLocalTags(repo)
	if err != nil {
		return "", fmt.Errorf("resolving ref %q in %q: listing tags: %w", expr, path, err)
	}
	hash, err := resolveSemver(tags, expr)
	if err != nil {
		return "", fmt.Errorf("resolving ref %q in %q: %w", expr, path, err)
	}
	return hash, nil
}

func listLocalTags(repo *git.Repository) (map[string]string, error) {
	iter, err := repo.Tags()
	if err != nil {
		return nil, err
	}
	defer iter.Close()
	tags := map[string]string{}
	if err := iter.ForEach(func(ref *plumbing.Reference) error {
		name := strings.TrimPrefix(ref.Name().String(), "refs/tags/")
		if name == "" {
			return nil
		}
		hash := ref.Hash().String()
		if peeled, err := repo.ResolveRevision(plumbing.Revision(ref.Name().String() + "^{commit}")); err == nil {
			hash = peeled.String()
		} else if tagObj, err := repo.TagObject(ref.Hash()); err == nil {
			hash = tagObj.Target.String()
		}
		tags[name] = hash
		return nil
	}); err != nil {
		return nil, err
	}
	return tags, nil
}

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

func headHash(repo *git.Repository) (string, error) {
	head, err := repo.Head()
	if err != nil {
		return "", fmt.Errorf("reading HEAD: %w", err)
	}
	return head.Hash().String(), nil
}

func matchHashPrefix(hashes map[string]string, expr string) (string, bool) {
	lowered := strings.ToLower(expr)
	if len(lowered) < minShortHashLen ||
		len(lowered) > maxShortHashLen {
		return "", false
	}
	for i := range len(lowered) {
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
	slices.SortFunc(candidates, func(a, b candidate) int {
		return a.version.Compare(b.version)
	})
	return candidates[len(candidates)-1].hash, nil
}
