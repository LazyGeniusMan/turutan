# My Template (template-author starter)

This project is a `turutan` template: `turutan bootstrap <source> <dir>`
renders it into a new project, and `turutan update` merges later template
changes back. It exists to be copied: every engine feature has a live
example here, commented in `.turutan.yml`.

## Layout

| Path                      | Kind                                              |
|---------------------------|---------------------------------------------------|
| `.turutan.yml`            | Manifest: every top-level key, commented          |
| `README.md`               | This author guide (verbatim file example)         |
| `LICENSE`                 | Verbatim license, planted via `license-files`     |
| `hello.txt`               | Verbatim file: `{{ }}` stays literal              |
| `greeting.txt.tmpl`       | Rendered file: inputs, sprig, `.profiles`        |
| `docs/guide.md.tmpl`      | Rendered file, planted via `doc-files`            |
| `optional/`               | Conditional layer (`includes` + `excludes`)       |
| `optional/local-*.txt`    | Conditional `preserves` (seed-once)               |
| `drafts/`, `scratch.log`  | Dropped by root `excludes`                        |
| `config/local.conf`       | Root `preserves`: rendered only when absent       |
| `new-name.txt`            | Rename target for `renames`                       |
| `hooks/post-setup.sh`     | Post hook script (needs `--allow-hooks`)          |
| `extra-banner.txt.tmpl`   | Bool-gated conditional file                       |

## Manifest tour

Top-level keys: `source` (advisory URL), `min-engine` (semver gate),
`license` plus `license-files` (planted verbatim into
`.config/turutan/`; empty `license` falls back to SPDX detection from
`TEMPLATE_LICENSE`, then `LICENSE`), `doc-files` (planted after
rendering, listed by rendered name), `includes` (whitelist; empty means
all), `excludes` (deny; `.git/`, `.config/turutan/**`, `.turutan.yml`
are always skipped), `preserves` (seed-once), `conflict`
(`inline`|`rej`), `hooks.pre/post`, `migrations[{from, run}]`,
`inputs`, `conditionals[{if, includes, excludes, preserves}]`,
`renames`.

Inputs: `string` with label/default/help/options (`greeting_style`),
`array` with options and comma-split answers (`profiles`, `features`),
`bool` answered `y/n` (`enable_extra`), and a conditional `if`
predicate (`extra_detail`, asked only when `enable_extra` is true).
`project_name`, `project_description`, `profiles` are asked first so
predicates over `.profiles` see answered values.

Conditionals and templates: `if` predicates plus every `*.tmpl` file
render with `missingkey=error` and sprig functions (minus
`env`/`expandenv`, plus slice-aware `contains`); `.profiles` defaults
to `[]`. The `*.tmpl` suffix is stripped on render, and both `x.tmpl`
and `x` match include/exclude globs. See `go doc text/template` for
data/dot/pipeline terms.

File ops: verbatim files copy byte-for-byte, `.tmpl` files render,
`includes`/`excludes`/`preserves` layer in order (a later layer can
re-include after an earlier exclude), `renames` forward-maps recorded
paths across releases, and per-run user content is kept with
`bootstrap --skip a,b` on top of `preserves`.

Hooks, migrations, min-engine: bootstrap runs `hooks.pre/post` after
publish, update runs `migrations` whose `from` is a semver range or a
7-64 hex commit prefix (empty matches all). Both need consent:
interactive prompt or `--allow-hooks`, refused in `--non-interactive`
without it. Hook environments filter secrets; migrations additionally
see `TURUTAN_OLD` and `TURUTAN_NEW`.

## Try it

```sh
# Scaffolded by: turutan create ./my-template
# Render with defaults (hooks skipped without consent):
turutan bootstrap --defaults ./my-template ./out
# Render with the example hooks and extra layer:
turutan bootstrap --defaults --allow-hooks ./my-template ./out
# Answer inputs from a file instead of prompts:
turutan bootstrap --non-interactive --allow-hooks --data-file values.yaml ./my-template ./out
```
