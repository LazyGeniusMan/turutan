// SPDX-License-Identifier: Apache-2.0

package scaffold

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/LazyGeniusMan/turutan/internal/config"
	"github.com/LazyGeniusMan/turutan/internal/filter"
	"github.com/LazyGeniusMan/turutan/internal/template"
	"github.com/Masterminds/semver/v3"
)

// Bootstrap scaffolds a new project from a template source:
//
//	parse URI → resolve ref → fetch → read .turutan.yml → answers
//	→ render → ignore/preserve → write state+lock → hooks consent gate.
//
// An empty source resolves the built-in remote-default URL. The target
// directory must be empty unless Force is set. In NonInteractive mode the
// caller must supply AnswersFile or Defaults, and hook-bearing templates
// are refused unless AllowHooks consents.
func Bootstrap(source, target string, opts Options) error {
	stdout, stderr, stdin := streams(opts)
	if err := opts.Conflict.Validate(); err != nil {
		return err
	}
	if err := filter.ValidateGlobs(opts.Skip); err != nil {
		return fmt.Errorf("bootstrap: bad --skip entry: %w", err)
	}
	if opts.NonInteractive && !opts.Defaults && opts.AnswersFile == "" {
		return fmt.Errorf("bootstrap: non-interactive mode requires --answers-file or --defaults")
	}
	src, err := template.ResolveAlias(source)
	if err != nil {
		return err
	}
	if opts.Ref != "" {
		src.RequestedRef = opts.Ref
	}
	if opts.Subpath != "" {
		src.Subpath = filepath.ToSlash(filepath.Clean(opts.Subpath))
	}
	vlogf(stderr, opts.Verbose, "bootstrap: source %q kind %s ref %q depth %d", src.String(), src.Kind, src.RequestedRef, src.Depth)
	// Local sources are stored as absolute paths: the state outlives the
	// bootstrap working directory, and check-update/diff re-resolve the
	// stored URI from inside the project. Remote locators are untouched.
	if src.Kind == template.KindLocalGit || src.Kind == template.KindFilesystem {
		abs, err := filepath.Abs(src.Repo)
		if err != nil {
			return fmt.Errorf("bootstrap: resolving source %q: %w", src.Raw, err)
		}
		src.Repo = abs
	}
	fetched, err := template.Fetch(src)
	if err != nil {
		return err
	}
	vlogf(stderr, opts.Verbose, "bootstrap: fetched %s commit %q", src.Kind, fetched.ResolvedCommit)
	if fetched.Cleanup != nil {
		defer fetched.Cleanup()
	}
	manifest, err := config.LoadManifest(os.DirFS(fetched.Dir))
	if err != nil {
		return err
	}
	engine, err := checkMinEngine(manifest, opts.Engine, stderr, opts.Verbose)
	if err != nil {
		return err
	}
	absTarget, err := prepareTarget(target, opts.Force)
	if err != nil {
		return err
	}
	answers, err := seedAnswers(absTarget, opts)
	if err != nil {
		return err
	}
	staged, cleanupStage, err := renderWithAnswers(fetched.Dir, answers, opts, stdout, stdin)
	if err != nil {
		return err
	}
	defer cleanupStage()
	files, err := publish(staged, absTarget, manifest, opts)
	if err != nil {
		return err
	}
	resolved := fetched.ResolvedCommit
	if resolved == "" {
		resolved = strings.TrimPrefix(config.ComputeManifestHash(files), "sha256:")
	}
	if err := writeStateAndLock(absTarget, src, resolved, answers, opts, files, engine); err != nil {
		return err
	}
	// The staged render is the pristine base for future 3-way updates.
	if err := StoreBase(absTarget, resolved, staged); err != nil {
		return fmt.Errorf("bootstrap: %w", err)
	}
	if err := gateHooks(manifest, opts, stdout, stdin); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "bootstrapped %s from %s @ %s (%d files)\n", absTarget, src.String(), shortSHA(resolved), len(files))
	vlogf(stderr, opts.Verbose, "bootstrap: wrote %d file(s) state %s lock %s", len(files), config.StateFileName, config.LockFileName)
	return nil
}

// streams resolves the effective IO streams, defaulting to the OS ones.
func streams(opts Options) (io.Writer, io.Writer, io.Reader) {
	stdout := opts.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	stderr := opts.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}
	stdin := opts.Stdin
	if stdin == nil {
		stdin = os.Stdin
	}
	return stdout, stderr, stdin
}

// checkMinEngine enforces the template min-engine floor before rendering
// and returns the normalized engine version for state. Dev builds (empty
// or unparsable version) skip the gate with a verbose stderr note.
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

// prepareTarget creates the target dir and enforces the empty-or---force
// guard, returning the absolute path.
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
		if err := os.MkdirAll(abs, 0o755); err != nil {
			return "", fmt.Errorf("bootstrap: creating target %q: %w", target, err)
		}
		return abs, nil
	}
	if len(entries) > 0 && !force {
		return "", fmt.Errorf("bootstrap: target %q is not empty (use --force to overwrite)", target)
	}
	return abs, nil
}

// renderWithAnswers renders srcDir into a temp staging dir, prompting for
// missing keys interactively (up to maxPromptAttempts) and failing with
// the key name in non-interactive mode.
func renderWithAnswers(srcDir string, answers map[string]any, opts Options, stdout io.Writer, stdin io.Reader) (string, func(), error) {
	stage, err := os.MkdirTemp("", "turutan-stage-*")
	if err != nil {
		return "", nil, fmt.Errorf("bootstrap: creating staging dir: %w", err)
	}
	cleanup := func() { os.RemoveAll(stage) }
	for range maxPromptAttempts {
		// Each attempt starts clean so retried renders never stack.
		if err := os.RemoveAll(stage); err != nil {
			return "", cleanup, fmt.Errorf("bootstrap: resetting staging dir: %w", err)
		}
		if err := os.MkdirAll(stage, 0o755); err != nil {
			return "", cleanup, fmt.Errorf("bootstrap: creating staging dir: %w", err)
		}
		err := template.RenderDir(srcDir, stage, answers)
		if err == nil {
			return stage, cleanup, nil
		}
		key := missingKey(err)
		if key == "" {
			return "", cleanup, err
		}
		if opts.NonInteractive {
			return "", cleanup, fmt.Errorf("bootstrap: missing answer for key %q (use --answers-file or --defaults): %w", key, err)
		}
		value, promptErr := promptValue(stdout, stdin, key)
		if promptErr != nil {
			return "", cleanup, promptErr
		}
		answers[key] = value
	}
	return "", cleanup, fmt.Errorf("bootstrap: too many missing answers (over %d prompts)", maxPromptAttempts)
}

// publish copies the staged render into target, honoring manifest ignore
// globs and keeping existing files matched by preserve or --skip. It
// returns the lock file entries for template-originated files.
func publish(staged, target string, manifest *config.Manifest, opts Options) ([]config.LockFile, error) {
	var files []config.LockFile
	err := filepath.WalkDir(staged, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(staged, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		slash := filter.ToSlash(rel)
		ignored, err := filter.MatchAny(manifest.Ignore, slash)
		if err != nil {
			return fmt.Errorf("bootstrap: matching ignore globs: %w", err)
		}
		if ignored {
			return nil
		}
		// Target access goes through lexical SafeJoin plus os.Root I/O so
		// symlinks escaping the target are refused instead of followed.
		// (d comes from our own staging dir; dst is the untrusted side.)
		if _, err := filter.SafeJoin(target, filepath.FromSlash(slash)); err != nil {
			return fmt.Errorf("bootstrap: %w", err)
		}
		if d.IsDir() {
			root, err := os.OpenRoot(target)
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
		if !d.Type().IsRegular() {
			return nil
		}
		if _, statErr := filter.StatWithinRoot(target, slash); statErr == nil {
			kept, err := keepExisting(slash, manifest, opts)
			if err != nil {
				return err
			}
			if kept {
				data, err := filter.ReadFileWithinRoot(target, slash)
				if err != nil {
					return err
				}
				files = append(files, config.FileEntry(slash, data))
				return nil
			}
			if !opts.Force {
				return fmt.Errorf("bootstrap: %q exists (use --force to overwrite)", slash)
			}
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if err := filter.WriteFileWithinRoot(target, slash, data, info.Mode().Perm()); err != nil {
			return fmt.Errorf("bootstrap: writing %q: %w", slash, err)
		}
		files = append(files, config.FileEntry(slash, data))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

// keepExisting reports whether an existing target file survives the
// bootstrap: preserve globs and --skip entries always prefer local content.
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

// writeStateAndLock records .turutan.json and .turutan.lock in target.
func writeStateAndLock(target string, src *template.Source, resolved string, answers map[string]any, opts Options, files []config.LockFile, engine string) error {
	state := &config.State{
		Version:         config.StateVersion,
		Template:        src.String(),
		SourceKind:      string(src.Kind),
		Subpath:         src.Subpath,
		RequestedRef:    src.RequestedRef,
		ResolvedCommit:  resolved,
		Answers:         answers,
		Skip:            opts.Skip,
		Engine:          "turutan/" + engine,
		TemplateLicense: config.TemplateLicenseMIT0,
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

// gateHooks enforces consent for template-declared hooks. M1 gates only:
// non-interactive runs refuse hooks unless --allow-hooks consents, and
// even consented hooks are reported as skipped (execution arrives later).
func gateHooks(manifest *config.Manifest, opts Options, stdout io.Writer, stdin io.Reader) error {
	hooks := append(append([]string{}, manifest.Hooks.Pre...), manifest.Hooks.Post...)
	if len(hooks) == 0 {
		return nil
	}
	if !opts.AllowHooks {
		if opts.NonInteractive {
			return fmt.Errorf("bootstrap: template declares hooks %q: refusing in --non-interactive mode without --allow-hooks", hooks)
		}
		if !promptConfirm(stdout, stdin, fmt.Sprintf("template declares hooks %q; allow", hooks)) {
			return fmt.Errorf("bootstrap: template declares hooks %q: consent declined", hooks)
		}
	}
	fmt.Fprintf(stdout, "turutan: template hooks %q declared but execution is not implemented in M1; skipping\n", hooks)
	return nil
}

// shortSHA abbreviates a hex identity for the summary line.
func shortSHA(sha string) string {
	if len(sha) > shortSHALen {
		return sha[:shortSHALen]
	}
	return sha
}
