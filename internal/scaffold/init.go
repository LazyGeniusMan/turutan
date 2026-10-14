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
	"github.com/LazyGeniusMan/turutan/internal/git"
	"github.com/LazyGeniusMan/turutan/internal/template"
	"github.com/Masterminds/semver/v3"
)

func Init(
	ctx context.Context,
	source, target string,
	opts Options,
) error {
	stdout, stderr, stdin := streams(opts)
	if err := validateInitOpts(opts); err != nil {
		return err
	}
	src, err := resolveInitSrc(source, opts, stderr)
	if err != nil {
		return err
	}
	fetched, manifest, engine, err := fetchInitTemplate(
		ctx, src, opts, stderr)
	if err != nil {
		return err
	}
	if fetched.Cleanup != nil {
		defer fetched.Cleanup()
	}
	absTarget, data, staged, cleanupStage, err := stageInit(
		target, fetched, manifest, opts, stdout, stdin)
	if err != nil {
		return err
	}
	defer cleanupStage()
	return finishInit(
		ctx, src, fetched, manifest, absTarget, data, staged,
		opts, engine, stdout, stderr, stdin)
}

func validateInitOpts(opts Options) error {
	if err := opts.Conflict.Validate(); err != nil {
		return err
	}
	if err := filter.ValidateGlobs(opts.Skip); err != nil {
		return fmt.Errorf("init: bad --skip entry: %w", err)
	}
	if opts.NonInteractive && !opts.Defaults && opts.DataFile == "" {
		return fmt.Errorf(
			"init: non-interactive mode requires " +
				"--data-file or --defaults")
	}
	return nil
}

func resolveInitSrc(
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
		"init: source %q kind %s ref %q depth %d",
		src.String(), src.Kind, src.RequestedRef, src.Depth)
	if src.Kind == template.KindLocalGit ||
		src.Kind == template.KindFilesystem {
		abs, err := filepath.Abs(src.Repo)
		if err != nil {
			return nil, fmt.Errorf(
				"init: resolving source %q: %w", src.Raw, err)
		}
		src.Repo = abs
	}
	return src, nil
}

func relativizeForStore(projectDir string, src *template.Source) template.Source {
	out := *src
	if out.Kind != template.KindFilesystem || out.Repo == "" ||
		!filepath.IsAbs(out.Repo) {
		return out
	}
	if rel, err := filepath.Rel(projectDir, out.Repo); err == nil {
		out.Repo = filepath.ToSlash(rel)
	}
	return out
}

func parseStoredSource(projectDir, raw string) (*template.Source, error) {
	if join, ok := storedRelativePath(projectDir, raw); ok {
		return template.ParseSource(join)
	}
	return template.ParseSource(raw)
}

func storedRelativePath(projectDir, raw string) (string, bool) {
	rest := raw
	if strings.HasPrefix(rest, "git::") {
		return "", false
	}
	if strings.Contains(rest, "://") || git.IsScpLike(rest) {
		return "", false
	}
	locator := rest
	if idx := strings.Index(rest, "?"); idx >= 0 {
		locator = rest[:idx]
	}
	if locator == "" || filepath.IsAbs(locator) ||
		filepath.IsAbs(filepath.FromSlash(locator)) {
		return "", false
	}
	return filepath.Join(projectDir, filepath.FromSlash(raw)), true
}

func fetchInitTemplate(
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
		"init: fetched %s commit %q", src.Kind, fetched.ResolvedCommit)
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

func stageInit(
	target string,
	fetched *template.Fetched,
	manifest *config.Manifest,
	opts Options,
	stdout io.Writer,
	stdin io.Reader,
) (string, map[string]any, string, func(), error) {
	absTarget, err := prepareTarget(target, opts.Force)
	if err != nil {
		return "", nil, "", nil, err
	}
	data, err := seedDataWithManifest(absTarget, opts, manifest)
	if err != nil {
		return "", nil, "", nil, err
	}
	staged, cleanup, err := renderWithData(
		fetched.Dir, data, manifest, opts, stdout, stdin)
	if err != nil {
		return "", nil, "", cleanup, err
	}
	if err := plantLicenseFiles(fetched.Dir, staged, manifest); err != nil {
		return "", nil, "", cleanup, fmt.Errorf("init: %w", err)
	}
	if err := plantDocFiles(staged, manifest); err != nil {
		return "", nil, "", cleanup, fmt.Errorf("init: %w", err)
	}
	return absTarget, data, staged, cleanup, nil
}

func finishInit(
	_ context.Context,
	src *template.Source,
	fetched *template.Fetched,
	manifest *config.Manifest,
	absTarget string,
	data map[string]any,
	staged string,
	opts Options,
	engine string,
	stdout, stderr io.Writer,
	stdin io.Reader,
) error {
	stored := relativizeForStore(absTarget, src)
	src = &stored
	preserved, err := preservedGlobs(data, manifest)
	if err != nil {
		return err
	}
	files, err := publish(staged, absTarget, manifest, opts, preserved)
	if err != nil {
		return err
	}
	resolved := fetched.ResolvedCommit
	if resolved == "" {
		resolved = strings.TrimPrefix(
			config.ComputeManifestHash(files), "sha256:")
	}
	license := resolveTemplateLicense(os.DirFS(fetched.Dir), manifest)
	vlogf(stderr, opts.Verbose, "init: template license %s", license)
	if err := writeStateAndLock(
		absTarget, src, resolved, data, opts, files, engine, license,
	); err != nil {
		return err
	}
	if err := StoreBase(absTarget, resolved, staged); err != nil {
		return fmt.Errorf("init: %w", err)
	}
	if err := gateHooks(manifest, absTarget, opts, stdout, stderr, stdin); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(stdout, "initialized %s from %s @ %s (%d files)\n",
		absTarget, src.String(), shortSHA(resolved), len(files)); err != nil {
		return err
	}
	vlogf(stderr, opts.Verbose,
		"init: wrote %d file(s) state %s lock %s",
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
		return "", fmt.Errorf("init: bad min-engine %q: %w", manifest.MinEngine, err)
	}
	current, err := semver.NewVersion(version)
	if err != nil {
		vlogf(stderr, verbose, "dev engine %q skips min-engine check %q", version, manifest.MinEngine)
		return version, nil
	}
	if !constraint.Check(current) {
		return "", fmt.Errorf("init: template requires engine %s: have %s", manifest.MinEngine, version)
	}
	return version, nil
}

func prepareTarget(target string, force bool) (string, error) {
	abs, err := filepath.Abs(target)
	if err != nil {
		return "", fmt.Errorf("init: resolving target %q: %w", target, err)
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("init: reading target %q: %w", target, err)
		}
		if err := os.MkdirAll(abs, 0o750); err != nil {
			return "", fmt.Errorf("init: creating target %q: %w", target, err)
		}
		return abs, nil
	}
	if len(entries) > 0 && !force {
		return "", fmt.Errorf("init: target %q is not empty (use --force to overwrite)", target)
	}
	return abs, nil
}

func renderWithData(
	srcDir string,
	data map[string]any,
	manifest *config.Manifest,
	opts Options,
	stdout io.Writer,
	stdin io.Reader,
) (string, func(), error) {
	stage, err := os.MkdirTemp("", "turutan-stage-*")
	if err != nil {
		return "", nil, fmt.Errorf("init: creating staging dir: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(stage) }
	for range maxInputAttempts {
		if err := ensureInputValues(data, manifest, opts.NonInteractive, opts.Defaults, stdout, stdin, "init"); err != nil {
			return "", cleanup, err
		}
		normalizeDataWithManifest(data, manifest)
		conds := manifestConditionals(manifest)
		if err := os.RemoveAll(stage); err != nil {
			return "", cleanup, fmt.Errorf("init: resetting staging dir: %w", err)
		}
		if err := os.MkdirAll(stage, 0o750); err != nil {
			return "", cleanup, fmt.Errorf("init: creating staging dir: %w", err)
		}
		view := renderDataView(data)
		err := template.RenderDirWithConditionals(srcDir, stage, view, manifest.Includes, manifest.Excludes, manifest.Preserves, conds)
		if err == nil {
			return stage, cleanup, nil
		}
		key, ok := AsMissingKey(err)
		if !ok {
			return "", cleanup, err
		}
		if opts.NonInteractive {
			return "", cleanup, fmt.Errorf(
				"init: missing data for key %q "+"(use --data-file or --defaults): %w",
				key, err)
		}
		if err := inputMissingKey(data, manifest, stdout, stdin, key); err != nil {
			return "", cleanup, err
		}
	}
	return "", cleanup, fmt.Errorf("init: too many missing values (over %d inputs)", maxInputAttempts)
}

func ensureInputValues(
	data map[string]any,
	manifest *config.Manifest,
	nonInteractive, defaults bool,
	stdout io.Writer,
	stdin io.Reader,
	op string,
) error {
	if manifest == nil {
		return nil
	}
	order := inputOrder(manifest)
	force := false
	for range maxInputAttempts {
		deferred := 0
		progressed := false
		for _, key := range order {
			if _, ok := data[key]; ok {
				continue
			}
			def := manifest.Inputs[key]
			ask, err := ShouldAskInput(manifest, key, renderDataView(data))
			if err != nil {
				if _, ok := AsMissingKey(err); ok {
					if !force {
						deferred++
						continue
					}
				} else {
					return err
				}
			} else if !ask {
				if def.Default != nil {
					data[key] = manifestDefaultCopy(def.Default)
					progressed = true
				}
				continue
			}
			if defaults && def.Default != nil {
				data[key] = manifestDefaultCopy(def.Default)
				progressed = true
				continue
			}
			if nonInteractive {
				return fmt.Errorf(
					"%s: missing value for key %q (use --data-file or --defaults)",
					op, key)
			}
			value, err := inputForKey(stdout, stdin, key, def)
			if err != nil {
				return err
			}
			data[key] = value
			progressed = true
		}
		if deferred == 0 {
			return nil
		}
		if !progressed {
			if force {
				return fmt.Errorf("%s: too many missing values (over %d inputs)", op, maxInputAttempts)
			}
			force = true
		}
	}
	return fmt.Errorf("%s: too many missing values (over %d inputs)", op, maxInputAttempts)
}

func inputMissingKey(
	data map[string]any,
	manifest *config.Manifest,
	stdout io.Writer,
	stdin io.Reader,
	key string,
) error {
	return inputMissingKeySeen(data, manifest, stdout, stdin, key, map[string]bool{})
}

func inputMissingKeySeen(
	data map[string]any,
	manifest *config.Manifest,
	stdout io.Writer,
	stdin io.Reader,
	key string,
	seen map[string]bool,
) error {
	if manifest != nil {
		if def, ok := manifest.Inputs[key]; ok {
			if seen[key] {
				return fmt.Errorf("circular input condition involving %q", key)
			}
			seen[key] = true
			ask, err := ShouldAskInput(manifest, key, renderDataView(data))
			if err != nil {
				if missing, ok := AsMissingKey(err); ok {
					if _, present := data[missing]; !present {
						if _, isInput := manifest.Inputs[missing]; isInput {
							if err := inputMissingKeySeen(data, manifest, stdout, stdin, missing, seen); err != nil {
								return err
							}
							ask, err = ShouldAskInput(manifest, key, renderDataView(data))
							if err != nil {
								return err
							}
						}
					}
				} else {
					return err
				}
			}
			if err == nil && !ask {
				if def.Default != nil {
					data[key] = manifestDefaultCopy(def.Default)
					return nil
				}
				return fmt.Errorf("template needs %q but it is skipped by its `if` condition and has no default", key)
			}
			value, err := inputForKey(stdout, stdin, key, def)
			if err != nil {
				return err
			}
			data[key] = value
			return nil
		}
	}
	value, err := inputValue(stdout, stdin, key)
	if err != nil {
		return err
	}
	data[key] = value
	return nil
}

func publish(
	staged, target string,
	manifest *config.Manifest,
	opts Options,
	preserved []string,
) ([]config.LockFile, error) {
	var files []config.LockFile
	pub := &publisher{
		staged: staged, target: target, manifest: manifest, opts: opts,
		preserved: preserved,
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
	staged    string
	target    string
	manifest  *config.Manifest
	opts      Options
	preserved []string
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
	if _, err := filter.SafeJoin(
		p.target, filepath.FromSlash(slash)); err != nil {
		return fmt.Errorf("init: %w", err)
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

func (p *publisher) publishDir(slash string) error {
	root, err := os.OpenRoot(p.target)
	if err != nil {
		return fmt.Errorf("init: opening target: %w", err)
	}
	mkdirErr := root.MkdirAll(filepath.FromSlash(slash), 0o755)
	closeErr := root.Close()
	if mkdirErr != nil {
		return fmt.Errorf("init: creating dir %q: %w", slash, mkdirErr)
	}
	if closeErr != nil {
		return fmt.Errorf("init: closing target: %w", closeErr)
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
		return fmt.Errorf("init: writing %q: %w", slash, err)
	}
	*files = append(*files, config.FileEntry(slash, data))
	return nil
}

func (p *publisher) keepOrFail(
	slash string,
	files *[]config.LockFile,
) (bool, error) {
	kept, err := keepExisting(slash, p.manifest, p.opts, p.preserved)
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
			"init: %q exists (use --force to overwrite)", slash)
	}
	return false, nil
}

func keepExisting(slash string, manifest *config.Manifest, opts Options, preserved []string) (bool, error) {
	kept, err := isPreservedPath(slash, manifest, preserved)
	if err != nil {
		return false, fmt.Errorf("init: %w", err)
	}
	if kept {
		return true, nil
	}
	skipped, err := filter.MatchAny(opts.Skip, slash)
	if err != nil {
		return false, fmt.Errorf("init: matching --skip entries: %w", err)
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
	_, _ = fmt.Fprintf(stderr,
		"turutan: template manifest source %q differs from requested %q (advisory only)\n",
		manifest.Source, src.String())
}

func writeStateAndLock(
	target string,
	src *template.Source,
	resolved string,
	data map[string]any,
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
		Data:            data,
		Skip:            opts.Skip,
		Conflict:        string(opts.Conflict),
		Engine:          "turutan/" + engine,
		TemplateLicense: license,
	}
	if err := config.SaveState(target, state); err != nil {
		return fmt.Errorf("init: %w", err)
	}
	lock := &config.Lock{
		Source:      src.String(),
		Ref:         src.RequestedRef,
		ResolvedSHA: resolved,
		Subpath:     src.Subpath,
		Files:       files,
	}
	if err := config.SaveLock(target, lock); err != nil {
		return fmt.Errorf("init: %w", err)
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
				"init: template declares hooks %q: refusing in "+"--non-interactive mode without --allow-hooks",
				hooks)
		}
		allow, err := askConfirm(stdout, stdin, fmt.Sprintf("template declares hooks %q; allow", hooks))
		if err != nil {
			return err
		}
		if !allow {
			if _, err := fmt.Fprintf(stdout, "turutan: template hooks %q skipped (consent declined)\n", hooks); err != nil {
				return err
			}
			return nil
		}
	}
	for _, hook := range hooks {
		if err := runHookCmd(target, hook, stderr); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(stdout, "turutan: ran %d template hook(s)\n", len(hooks)); err != nil {
		return err
	}
	return nil
}

func runHookCmd(target, hook string, stderr io.Writer) error {
	if hook == "" {
		return fmt.Errorf("init: hook has an empty command")
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
		if _, err := fmt.Fprintf(stderr, "turutan: hook %q output:\n%s", hook, output); err != nil {
			return err
		}
	}
	if err != nil {
		return fmt.Errorf("init: hook %q failed: %w", hook, err)
	}
	return nil
}

func shortSHA(sha string) string {
	if len(sha) > shortSHALen {
		return sha[:shortSHALen]
	}
	return sha
}
