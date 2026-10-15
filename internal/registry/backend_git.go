// SPDX-License-Identifier: Apache-2.0

package registry

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	gitgo "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing/client"
	"github.com/go-git/go-git/v6/plumbing/object"
	githttp "github.com/go-git/go-git/v6/plumbing/transport/http"
	gitssh "github.com/go-git/go-git/v6/plumbing/transport/ssh"

	"github.com/LazyGeniusMan/turutan/internal/git"
)

type GitBackend struct {
	operator GitOperator
}

type GitOperator interface {
	Clone(ctx context.Context, url, ref, dir string, depth int) (string, error)
	Checkout(dir, sha string) error
	CommitTag(dir, ref string) (string, error)
	Push(ctx context.Context, dir, ref string, uri URI, auth AuthConfig) error
}

type defaultGitOperator struct{}

func (defaultGitOperator) Clone(ctx context.Context, url, ref, dir string, depth int) (string, error) {
	return git.Clone(ctx, url, ref, dir, depth)
}

func (defaultGitOperator) Checkout(dir, sha string) error {
	return git.Checkout(dir, sha)
}

func (defaultGitOperator) CommitTag(dir, ref string) (string, error) {
	return commitTagGitDir(dir, ref)
}

func (defaultGitOperator) Push(ctx context.Context, dir, ref string, uri URI, auth AuthConfig) error {
	return pushGitDir(ctx, dir, ref, uri, auth)
}

func NewGitBackend(operator GitOperator) *GitBackend {
	if operator == nil {
		operator = defaultGitOperator{}
	}
	return &GitBackend{operator: operator}
}

func (b *GitBackend) Scheme() Scheme { return SchemeGit }

func (b *GitBackend) Bundle(ctx context.Context, srcDir string, uri URI, auth AuthConfig) (string, error) {
	if err := requireScheme(uri, SchemeGit); err != nil {
		return "", err
	}
	if err := validGitTag(uri.Ref); err != nil {
		return "", fmt.Errorf("bundling %q: %w", uri.String(), err)
	}
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("bundling %q: %w", uri.String(), err)
	}
	info, err := os.Stat(srcDir)
	if err != nil {
		return "", fmt.Errorf("bundling %q: reading source: %w", uri.String(), err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("bundling %q: source is not a directory", uri.String())
	}
	digest, err := DigestDir(srcDir)
	if err != nil {
		return "", fmt.Errorf("bundling %q: %w", uri.String(), err)
	}
	tmp, err := os.MkdirTemp("", "turutan-git-bundle-*")
	if err != nil {
		return "", fmt.Errorf("bundling %q: creating temp dir: %w", uri.String(), err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	remote := gitRemoteURL(uri)
	if _, err := b.operator.Clone(ctx, remote, "", tmp, git.ShallowDepth); err != nil {
		return "", fmt.Errorf("bundling %q: %w", uri.String(), err)
	}
	target := tmp
	if uri.Subpath != "" {
		sub, err := joinSubpath(tmp, uri.Subpath)
		if err != nil {
			return "", fmt.Errorf("bundling %q: %w", uri.String(), err)
		}
		if err := os.RemoveAll(sub); err != nil {
			return "", fmt.Errorf("bundling %q: clearing subpath: %w", uri.String(), err)
		}
		target = sub
	}
	if err := CopyTree(srcDir, target); err != nil {
		return "", fmt.Errorf("bundling %q: %w", uri.String(), err)
	}
	if _, err := b.operator.CommitTag(tmp, uri.Ref); err != nil {
		return "", fmt.Errorf("bundling %q: %w", uri.String(), err)
	}
	if err := b.operator.Push(ctx, tmp, uri.Ref, uri, auth); err != nil {
		return "", fmt.Errorf("bundling %q: %w", uri.String(), err)
	}
	return digest, nil
}

func (b *GitBackend) Fetch(ctx context.Context, uri URI, _ AuthConfig, destDir string) error {
	if err := requireScheme(uri, SchemeGit); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("fetching %q: %w", uri.String(), err)
	}
	tmp, err := os.MkdirTemp("", "turutan-git-fetch-*")
	if err != nil {
		return fmt.Errorf("fetching %q: creating temp dir: %w", uri.String(), err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	remote := gitRemoteURL(uri)
	if _, err := b.operator.Clone(ctx, remote, uri.Ref, tmp, git.ShallowDepth); err != nil {
		return fmt.Errorf("fetching %q: %w", uri.String(), err)
	}
	source := tmp
	if uri.Subpath != "" {
		sub, err := joinSubpath(tmp, uri.Subpath)
		if err != nil {
			return fmt.Errorf("fetching %q: %w", uri.String(), err)
		}
		info, err := os.Stat(sub)
		if err != nil {
			return fmt.Errorf("fetching %q: subpath: %w", uri.String(), err)
		}
		if !info.IsDir() {
			return fmt.Errorf("fetching %q: subpath is not a directory", uri.String())
		}
		source = sub
	}
	if err := CopyTree(source, destDir); err != nil {
		return fmt.Errorf("fetching %q: %w", uri.String(), err)
	}
	return nil
}

func gitRemoteURL(uri URI) string {
	host := uri.Host
	if uri.User != "" {
		host = uri.User + "@" + host
	}
	if uri.Transport == "git" {
		return "git://" + host + "/" + uri.Path
	}
	return uri.Transport + "://" + host + "/" + uri.Path
}

func joinSubpath(repoDir, subpath string) (string, error) {
	joined := filepath.Join(repoDir, filepath.FromSlash(subpath))
	abs, err := filepath.Abs(joined)
	if err != nil {
		return "", fmt.Errorf("resolving subpath %q: %w", subpath, err)
	}
	root, err := filepath.Abs(repoDir)
	if err != nil {
		return "", fmt.Errorf("resolving repository dir: %w", err)
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return "", fmt.Errorf("relativizing subpath %q: %w", subpath, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("subpath %q escapes the repository", subpath)
	}
	return abs, nil
}

func disableGitSigning(repo *gitgo.Repository, dir string) error {
	cfg, err := repo.Config()
	if err != nil {
		return fmt.Errorf("reading git config of %q: %w", dir, err)
	}
	cfg.Commit.GpgSign = config.NewOptBool(false)
	cfg.Tag.GpgSign = config.NewOptBool(false)
	if err := repo.SetConfig(cfg); err != nil {
		return fmt.Errorf("disabling git signing in %q: %w", dir, err)
	}
	return nil
}

func validGitTag(ref string) error {
	if ref == "" {
		return fmt.Errorf("ref is empty: Bundle needs a tag such as templates/<name>/<version>")
	}
	if strings.ContainsAny(ref, " \t\n~^:?*[]\\") || strings.Contains(ref, "..") {
		return fmt.Errorf("ref %q is not a valid git tag", ref)
	}
	return nil
}

func commitTagGitDir(dir, ref string) (string, error) {
	repo, err := gitgo.PlainOpen(dir)
	if err != nil {
		return "", fmt.Errorf("opening git repository %q: %w", dir, err)
	}
	if err := disableGitSigning(repo, dir); err != nil {
		return "", err
	}
	worktree, err := repo.Worktree()
	if err != nil {
		return "", fmt.Errorf("opening worktree of %q: %w", dir, err)
	}
	if err := worktree.AddWithOptions(&gitgo.AddOptions{All: true}); err != nil {
		return "", fmt.Errorf("staging template tree in %q: %w", dir, err)
	}
	signature := &object.Signature{Name: "turutan", Email: "turutan@localhost", When: time.Now()}
	hash, err := worktree.Commit("turutan: publish template bundle", &gitgo.CommitOptions{Author: signature})
	if err != nil {
		return "", fmt.Errorf("committing template tree in %q: %w", dir, err)
	}
	if _, err := repo.CreateTag(ref, hash, &gitgo.CreateTagOptions{
		Tagger:  signature,
		Message: "turutan template bundle",
	}); err != nil {
		return "", fmt.Errorf("tagging %q in %q: %w", ref, dir, err)
	}
	return hash.String(), nil
}

func pushGitDir(ctx context.Context, dir, ref string, uri URI, auth AuthConfig) error {
	repo, err := gitgo.PlainOpen(dir)
	if err != nil {
		return fmt.Errorf("opening git repository %q: %w", dir, err)
	}
	opts, err := gitPushClientOptions(uri, auth)
	if err != nil {
		return err
	}
	spec := config.RefSpec("refs/tags/" + ref + ":refs/tags/" + ref)
	if err := repo.PushContext(ctx, &gitgo.PushOptions{
		RemoteName:    "origin",
		RefSpecs:      []config.RefSpec{spec},
		ClientOptions: opts,
	}); err != nil {
		return fmt.Errorf("pushing tag %q: %w", ref, err)
	}
	return nil
}

func gitPushClientOptions(uri URI, auth AuthConfig) ([]client.Option, error) {
	switch uri.Transport {
	case "http", "https":
		switch auth.Kind {
		case AuthKindBasic:
			username, password, _ := auth.BasicAuth()
			if username == "" {
				username = uri.User
			}
			return []client.Option{client.WithHTTPAuth(&githttp.BasicAuth{Username: username, Password: password})}, nil
		case AuthKindToken:
			token, _ := auth.BearerToken()
			username := auth.Username
			if username == "" {
				username = "x-access-token"
			}
			return []client.Option{client.WithHTTPAuth(&githttp.BasicAuth{Username: username, Password: token})}, nil
		default:
			return nil, nil
		}
	case "ssh", "git":
		if auth.Kind != AuthKindSSH || auth.SSHKey == "" {
			return nil, fmt.Errorf("pushing over %q needs AuthConfig SSHKey material", uri.Transport)
		}
		user := uri.User
		if user == "" {
			user = "git"
		}
		public, err := gitssh.NewPublicKeys(user, []byte(auth.SSHKey), auth.Password)
		if err != nil {
			return nil, fmt.Errorf("parsing SSH key: %w", err)
		}
		return []client.Option{client.WithSSHAuth(public)}, nil
	default:
		return nil, fmt.Errorf("unsupported git transport %q", uri.Transport)
	}
}
