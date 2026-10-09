# turutan

Manage project-template lifecycle: scaffold new projects from versioned templates, then merge template updates back into them.

## Quickstart

```sh
go build -o turutan ./cmd/turutan

# Scaffold from the built-in default (a remote-git source in this repo).
turutan bootstrap git::https://github.com/LazyGeniusMan/turutan.git//templates/default?ref=templates-default/v1 ./myapp --defaults
# Bare source resolves the same remote default; a local path works offline.
turutan bootstrap ./templates/default ./myapp --defaults

cd myapp
turutan check-update   # exit 0 up-to-date, 2 update available
turutan diff           # exit 0 no drift, 2 drift found
turutan update         # merge template changes (--conflict rej for .rej files)
turutan version        # version/commit/date plus license notice
```

Releases stamp the version via `go build -ldflags "-X main.version=$V -X main.commit=$C -X main.date=$D"`
(GoReleaser does this automatically; dev builds report `dev/none/unknown`).

## Remote authentication

Git transport credentials come from the environment only and are never
logged. SSH remotes try `TURUTAN_SSH_KEY` (PEM content or key-file path),
then `TURUTAN_SSH_PASSWORD`, then the ssh-agent; HTTPS remotes use
`GITHUB_TOKEN` as `x-access-token` credentials when set. Host-key (`known_hosts`)
and TLS verification is strict by default; `TURUTAN_INSECURE_SKIP_VERIFY=1`
disables it with a loud warning (never in production).

## Self-hosting

This repo's own skeleton comes from the default template: the M4 e2e gate
(`internal/scaffold/selfhost_integration_test.go`, `//go:build integration`)
bootstraps `./templates/default`, asserts `turutan diff` exits 0 on the clean
checkout, and runs the check-update → edit → diff → update cycle in `t.TempDir()`:

```sh
go test -short ./...                    # unit suite (offline)
go test -short -tags=integration ./...  # e2e offline steps; remote parity skips
go test -tags=integration ./...         # full e2e incl. remote render parity
```

## License

Dual-licensed: the CLI and all engine code is Apache-2.0 (root `LICENSE`);
the default template and files generated from it are MIT-0
(`templates/default/LICENSE`; generated `.turutan.json` records
`templateLicense: "MIT-0"`). Every release states both licenses.
Dependency licenses are audited manually at release time:

```sh
go run github.com/google/go-licenses@latest check ./...
```
