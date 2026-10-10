// SPDX-License-Identifier: Apache-2.0

package scaffold

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/LazyGeniusMan/turutan/internal/config"
	"github.com/LazyGeniusMan/turutan/internal/filter"
	"github.com/LazyGeniusMan/turutan/internal/template"
	"github.com/Masterminds/semver/v3"
)

func Bootstrap(
	ctx context.Context,
	source, target string,
	opts Options,
) error {
	stdout, stderr, stdin := streams(opts)
	if err := validateBootstrapOpts(opts); err != nil {
		return err
	}
	src, err := resolveBootstrapSrc(source, opts, stderr)
	if err != nil {
		return err
	}
	fetched, manifest, engine, err := fetchBootstrapTemplate(
		ctx, src, opts, stderr)
	if err != nil {
		return err
	}
	if fetched.Cleanup != nil {
		defer fetched.Cleanup()
	}
	absTarget, answers, staged, cleanupStage, err := stageBootstrap(
		target, fetched, opts, stdout, stdin)
	if err != nil {
		return err
	}
	defer cleanupStage()
	return finishBootstrap(
		ctx, src, fetched, manifest, absTarget, answers, staged,
		opts, engine, stdout, stderr, stdin)
}

func validateBootstrapOpts(opts Options) error {
	if err := opts.Conflict.Validate(); err != nil {
		return err
	}
	if err := filter.ValidateGlobs(opts.Skip); err != nil {
		return fmt.Errorf("bootstrap: bad --skip entry: %w", err)
	}
	if opts.NonInteractive && !opts.Defaults && opts.AnswersFile == "" {
		return fmt.Errorf(
			"bootstrap: non-interactive mode requires " +
				"--answers-file or --defaults")
	}
	return nil
}

func resolveBootstrapSrc(
	source string,
	opts Options,
	stderr io.Writer,
) (*template.Source, error) {
	src, err := template.ResolveAlias(source)
	if err != nil {
		return nil, err
	}
	if opts.Ref != "" {
		src.RequestedRef = opts.Ref
	}
	if opts.Subpath != "" {
		src.Subpath = filepath.ToSlash(filepath.Clean(opts.Subpath))
	}
	vlogf(stderr, opts.Verbose,
		"bootstrap: source %q kind %s ref %q depth %d",
		src.String(), src.Kind, src.RequestedRef, src.Depth)
	if src.Kind == template.KindLocalGit ||
		src.Kind == template.KindFilesystem {
		abs, err := filepath.Abs(src.Repo)
		if err != nil {
			return nil, fmt.Errorf(
				"bootstrap: resolving source %q: %w", src.Raw, err)
		}
		src.Repo = abs
	}
	return src, nil
}

func fetchBootstrapTemplate(
	ctx context.Context,
	src *template.Source,
	opts Options,
	stderr io.Writer,
) (*template.Fetched, *config.Manifest, string, error) {
	fetched, err := template.Fetch(ctx, src)
	if err != nil {
		return nil, nil, "", err
	}
	vlogf(stderr, opts.Verbose,
		"bootstrap: fetched %s commit %q", src.Kind, fetched.ResolvedCommit)
	manifest, err := config.LoadManifest(os.DirFS(fetched.Dir))
	if err != nil {
		if fetched.Cleanup != nil {
			fetched.Cleanup()
		}
		return nil, nil, "", err
	}
	warnManifestSourceMismatch(stderr, manifest, src)
	engine, err := checkMinEngine(manifest, opts.Engine, stderr, opts.Verbose)
	if err != nil {
		if fetched.Cleanup != nil {
			fetched.Cleanup()
		}
		return nil, nil, "", err
	}
	return fetched, manifest, engine, nil
}

func stageBootstrap(
	target string,
	fetched *template.Fetched,
	opts Options,
	stdout io.Writer,
	stdin io.Reader,
) (string, map[string]any, string, func(), error) {
	absTarget, err := prepareTarget(target, opts.Force)
	if err != nil {
		return "", nil, "", nil, err
	}
	answers, err := seedAnswers(absTarget, opts)
	if err != nil {
		return "", nil, "", nil, err
	}
	staged, cleanup, err := renderWithAnswers(
		fetched.Dir, answers, opts, stdout, stdin)
	if err != nil {
		return "", nil, "", cleanup, err
	}
	return absTarget, answers, staged, cleanup, nil
}

func finishBootstrap(
	_ context.Context,
	src *template.Source,
	fetched *template.Fetched,
	manifest *config.Manifest,
	absTarget string,
	answers map[string]any,
	staged string,
	opts Options,
	engine string,
	stdout, stderr io.Writer,
	stdin io.Reader,
) error {
	files, err := publish(staged, absTarget, manifest, opts)
	if err != nil {
		return err
	}
	resolved := fetched.ResolvedCommit
	if resolved == "" {
		resolved = strings.TrimPrefix(
			config.ComputeManifestHash(files), "sha256:")
	}
	license := resolveTemplateLicense(os.DirFS(fetched.Dir))
	vlogf(stderr, opts.Verbose, "bootstrap: template license %s", license)
	if err := writeStateAndLock(
		absTarget, src, resolved, answers, opts, files, engine, license,
	); err != nil {
		return err
	}
	if err := StoreBase(absTarget, resolved, staged); err != nil {
		return fmt.Errorf("bootstrap: %w", err)
	}
	if err := gateHooks(manifest, absTarget, opts, stdout, stderr, stdin); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "bootstrapped %s from %s @ %s (%d files)\n",
		absTarget, src.String(), shortSHA(resolved), len(files))
	vlogf(stderr, opts.Verbose,
		"bootstrap: wrote %d file(s) state %s lock %s",
		len(files), config.StateFileName, config.LockFileName)
	return nil
}

func streams(opts Options) (io.Writer, io.Writer, io.Reader) {
	return resolveStreams(opts.Stdout, opts.Stderr, opts.Stdin)
}

func checkMinEngine(manifest *config.Manifest, engine string, stderr io.Writer, verbose bool) (string, error) {
	version := strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(engine), "turutan/"), "v")
	if version == "" {
		version = "dev"
	}
	if manifest.MinEngine == "" {
		return version, nil
	}
	constraint, err := semver.NewConstraint(manifest.MinEngine)
	if err != nil {
		return "", fmt.Errorf("bootstrap: bad min-engine %q: %w", manifest.MinEngine, err)
	}
	current, err := semver.NewVersion(version)
	if err != nil {
		vlogf(stderr, verbose, "dev engine %q skips min-engine check %q", version, manifest.MinEngine)
		return version, nil
	}
	if !constraint.Check(current) {
		return "", fmt.Errorf("bootstrap: template requires engine %s: have %s", manifest.MinEngine, version)
	}
	return version, nil
}

func prepareTarget(target string, force bool) (string, error) {
	abs, err := filepath.Abs(target)
	if err != nil {
		return "", fmt.Errorf("bootstrap: resolving target %q: %w", target, err)
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("bootstrap: reading target %q: %w", target, err)
		}
		if err := os.MkdirAll(abs, 0o750); err != nil {
			return "", fmt.Errorf("bootstrap: creating target %q: %w", target, err)
		}
		return abs, nil
	}
	if len(entries) > 0 && !force {
		return "", fmt.Errorf("bootstrap: target %q is not empty (use --force to overwrite)", target)
	}
	return abs, nil
}

func renderWithAnswers(
	srcDir string,
	answers map[string]any,
	opts Options,
	stdout io.Writer,
	stdin io.Reader,
) (string, func(), error) {
	stage, err := os.MkdirTemp("", "turutan-stage-*")
	if err != nil {
		return "", nil, fmt.Errorf("bootstrap: creating staging dir: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(stage) }
	for range maxPromptAttempts {
		if err := os.RemoveAll(stage); err != nil {
			return "", cleanup, fmt.Errorf("bootstrap: resetting staging dir: %w", err)
		}
		if err := os.MkdirAll(stage, 0o750); err != nil {
			return "", cleanup, fmt.Errorf("bootstrap: creating staging dir: %w", err)
		}
		err := template.RenderDir(srcDir, stage, answers)
		if err == nil {
			return stage, cleanup, nil
		}
		key, ok := AsMissingKey(err)
		if !ok {
			return "", cleanup, err
		}
		if opts.NonInteractive {
			return "", cleanup, fmt.Errorf(
				"bootstrap: missing answer for key %q "+"(use --answers-file or --defaults): %w",
				key, err)
		}
		value, promptErr := promptValue(stdout, stdin, key)
		if promptErr != nil {
			return "", cleanup, promptErr
		}
		answers[key] = value
	}
	return "", cleanup, fmt.Errorf("bootstrap: too many missing answers (over %d prompts)", maxPromptAttempts)
}

func publish(
	staged, target string,
	manifest *config.Manifest,
	opts Options,
) ([]config.LockFile, error) {
	var files []config.LockFile
	pub := &publisher{
		staged: staged, target: target, manifest: manifest, opts: opts,
	}
	err := filepath.WalkDir(staged, func(path string, d fs.DirEntry, err error) error {
		return pub.publishEntry(path, d, err, &files)
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

type publisher struct {
	staged   string
	target   string
	manifest *config.Manifest
	opts     Options
}

func (p *publisher) publishEntry(
	path string,
	d fs.DirEntry,
	walkErr error,
	files *[]config.LockFile,
) error {
	if walkErr != nil {
		return walkErr
	}
	rel, err := filepath.Rel(p.staged, path)
	if err != nil {
		return err
	}
	if rel == "." {
		return nil
	}
	slash := filter.ToSlash(rel)
	if skip, err := p.ignored(slash); err != nil || skip {
		return err
	}
	if _, err := filter.SafeJoin(
		p.target, filepath.FromSlash(slash)); err != nil {
		return fmt.Errorf("bootstrap: %w", err)
	}
	if d.IsDir() {
		return p.publishDir(slash)
	}
	if d.Type()&fs.ModeSymlink != 0 {
		return nil
	}
	if skip, err := p.skipSpecial(d); err != nil || skip {
		return err
	}
	return p.publishFile(path, d, slash, files)
}

func (p *publisher) ignored(slash string) (bool, error) {
	ignored, err := filter.MatchAny(p.manifest.Ignore, slash)
	if err != nil {
		return false, fmt.Errorf(
			"bootstrap: matching ignore globs: %w", err)
	}
	return ignored, nil
}

func (p *publisher) publishDir(slash string) error {
	root, err := os.OpenRoot(p.target)
	if err != nil {
		return fmt.Errorf("bootstrap: opening target: %w", err)
	}
	mkdirErr := root.MkdirAll(filepath.FromSlash(slash), 0o755)
	closeErr := root.Close()
	if mkdirErr != nil {
		return fmt.Errorf("bootstrap: creating dir %q: %w", slash, mkdirErr)
	}
	if closeErr != nil {
		return fmt.Errorf("bootstrap: closing target: %w", closeErr)
	}
	return nil
}

func (p *publisher) skipSpecial(d fs.DirEntry) (bool, error) {
	info, err := d.Info()
	if err != nil {
		return false, err
	}
	if filter.IsSpecialFile(info) || !info.Mode().IsRegular() {
		return true, nil
	}
	return false, nil
}

func (p *publisher) publishFile(
	path string,
	d fs.DirEntry,
	slash string,
	files *[]config.LockFile,
) error {
	if _, statErr := filter.StatWithinRoot(p.target, slash); statErr == nil {
		done, err := p.keepOrFail(slash, files)
		if err != nil || done {
			return err
		}
	}
	data, err := os.ReadFile(path) // #nosec G304 -- reads staged render file produced by this run's WalkDir traversal, not caller-controlled inclusion
	if err != nil {
		return err
	}
	info, err := d.Info()
	if err != nil {
		return err
	}
	if err := filter.WriteFileWithinRoot(
		p.target, slash, data, info.Mode().Perm()); err != nil {
		return fmt.Errorf("bootstrap: writing %q: %w", slash, err)
	}
	*files = append(*files, config.FileEntry(slash, data))
	return nil
}

func (p *publisher) keepOrFail(
	slash string,
	files *[]config.LockFile,
) (bool, error) {
	kept, err := keepExisting(slash, p.manifest, p.opts)
	if err != nil {
		return false, err
	}
	if kept {
		data, err := filter.ReadFileWithinRoot(p.target, slash)
		if err != nil {
			return false, err
		}
		*files = append(*files, config.FileEntry(slash, data))
		return true, nil
	}
	if !p.opts.Force {
		return false, fmt.Errorf(
			"bootstrap: %q exists (use --force to overwrite)", slash)
	}
	return false, nil
}

func keepExisting(slash string, manifest *config.Manifest, opts Options) (bool, error) {
	preserved, err := filter.MatchAny(manifest.Preserve, slash)
	if err != nil {
		return false, fmt.Errorf("bootstrap: matching preserve globs: %w", err)
	}
	if preserved {
		return true, nil
	}
	skipped, err := filter.MatchAny(opts.Skip, slash)
	if err != nil {
		return false, fmt.Errorf("bootstrap: matching --skip entries: %w", err)
	}
	return skipped, nil
}

func warnManifestSourceMismatch(stderr io.Writer, manifest *config.Manifest, src *template.Source) {
	if manifest.Source == "" || manifest.Source == src.String() {
		return
	}
	if stderr == nil {
		stderr = os.Stderr
	}
	fmt.Fprintf(stderr,
		"turutan: template manifest source %q differs from requested %q (advisory only)\n",
		manifest.Source, src.String())
}

func writeStateAndLock(
	target string,
	src *template.Source,
	resolved string,
	answers map[string]any,
	opts Options,
	files []config.LockFile,
	engine, license string,
) error {
	state := &config.State{
		Version:         config.StateVersion,
		Template:        src.String(),
		SourceKind:      string(src.Kind),
		Subpath:         src.Subpath,
		RequestedRef:    src.RequestedRef,
		ResolvedCommit:  resolved,
		Answers:         answers,
		Skip:            opts.Skip,
		Conflict:        string(opts.Conflict),
		Engine:          "turutan/" + engine,
		TemplateLicense: license,
	}
	if err := config.SaveState(target, state); err != nil {
		return fmt.Errorf("bootstrap: %w", err)
	}
	lock := &config.Lock{
		Source:      src.String(),
		Ref:         src.RequestedRef,
		ResolvedSHA: resolved,
		Subpath:     src.Subpath,
		Files:       files,
	}
	if err := config.SaveLock(target, lock); err != nil {
		return fmt.Errorf("bootstrap: %w", err)
	}
	return nil
}

func gateHooks(
	manifest *config.Manifest,
	target string,
	opts Options,
	stdout, stderr io.Writer,
	stdin io.Reader,
) error {
	hooks := append(append([]string{}, manifest.Hooks.Pre...), manifest.Hooks.Post...)
	if len(hooks) == 0 {
		return nil
	}
	if !opts.AllowHooks {
		if opts.NonInteractive {
			return fmt.Errorf(
				"bootstrap: template declares hooks %q: refusing in "+"--non-interactive mode without --allow-hooks",
				hooks)
		}
		if !promptConfirm(stdout, stdin, fmt.Sprintf("template declares hooks %q; allow", hooks)) {
			fmt.Fprintf(stdout, "turutan: template hooks %q skipped (consent declined)\n", hooks)
			return nil
		}
	}
	for _, hook := range hooks {
		if err := runHookCmd(target, hook, stderr); err != nil {
			return err
		}
	}
	fmt.Fprintf(stdout, "turutan: ran %d template hook(s)\n", len(hooks))
	return nil
}

func runHookCmd(target, hook string, stderr io.Writer) error {
	if hook == "" {
		return fmt.Errorf("bootstrap: hook has an empty command")
	}
	argv := []string{"-c", hook}
	if !strings.ContainsAny(hook, " \t\n|&;()<>$`\"'") {
		if safe, joinErr := filter.SafeJoin(target, filepath.FromSlash(hook)); joinErr == nil {
			if info, err := os.Stat(safe); err == nil && !info.IsDir() {
				argv = []string{safe}
			}
		}
	}
	executed := exec.Command("sh", argv...) // #nosec G204 -- runs template-declared hook gated by --allow-hooks consent with filtered env (no secrets)
	executed.Dir = target
	executed.Env = hookEnv()
	output, err := executed.CombinedOutput()
	if len(output) > 0 {
		fmt.Fprintf(stderr, "turutan: hook %q output:\n%s", hook, output)
	}
	if err != nil {
		return fmt.Errorf("bootstrap: hook %q failed: %w", hook, err)
	}
	return nil
}

func shortSHA(sha string) string {
	if len(sha) > shortSHALen {
		return sha[:shortSHALen]
	}
	return sha
}
