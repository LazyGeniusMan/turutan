# turutan default template

Minimal Go project skeleton rendered by `turutan bootstrap`. The
`{{.project_name}}` answer seeds the module and greeting (it defaults to
the target directory name with `--defaults`).

## Layout

| Template file | Renders to | Purpose |
| --- | --- | --- |
| `go.mod.tmpl` | `go.mod` | Module `{{.project_name}}`, go 1.26 |
| `cmd/app/main.go.tmpl` | `cmd/app/main.go` | Entry point, prints `app.Greet()` |
| `internal/app/app.go.tmpl` | `internal/app/app.go` | `Greet` returning the project name |
| `.turutan.yml` | `.turutan.yml` | Manifest: `min-engine`, ignore/preserve, `conflict: inline` |
| `hooks/` | `hooks/` | Placeholder (`.gitkeep`); hook execution is deferred to M3 |
| `TEMPLATE_LICENSE` | `TEMPLATE_LICENSE` | MIT-0 grant; SPDX recorded in `.turutan.json` |

`.turutan.yml` carries commented `migrations:` / `hooks:` placeholders.
Both stay empty: declaring either refuses `--non-interactive` runs
without `--allow-hooks`.

## MIN-ENGINE

The manifest requires engine `>=0.1.0`. Bootstrap and update below the
floor fail before rendering.

## Usage

```sh
turutan bootstrap ./templates/default ./myapp --defaults --non-interactive
cd myapp && go build ./...
turutan diff            # exit 0: clean checkout has no drift
turutan check-update    # exit 0: template and project identities match
```
