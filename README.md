# turutan

[![License](https://img.shields.io/github/license/LazyGeniusMan/turutan)](./LICENSE) [![CI](https://github.com/LazyGeniusMan/turutan/actions/workflows/ci.yml/badge.svg)](https://github.com/LazyGeniusMan/turutan/actions/workflows/ci.yml) [![Go Version](https://img.shields.io/github/go-mod/go-version/LazyGeniusMan/turutan)](https://go.dev/)

Manage project-template lifecycle: scaffold new projects from versioned templates, then merge template updates back into them.

## Installation

Prerequisites: [mise](https://mise.jdx.dev/)

```sh
git clone https://github.com/LazyGeniusMan/turutan.git
cd turutan
mise run pre-push:all
```

## Quickstart

```sh
mise run pre-push:all   # release-style static binary with version ldflags

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

## Remote authentication

Git transport credentials come from the environment only and are never
logged. SSH remotes try `TURUTAN_SSH_KEY` (PEM content or key-file path),
then `TURUTAN_SSH_PASSWORD`, then the ssh-agent; HTTPS remotes use
`GITHUB_TOKEN` as `x-access-token` credentials when set. Host-key (`known_hosts`)
and TLS verification is strict by default; `TURUTAN_INSECURE_SKIP_VERIFY=1`
disables it with a loud warning (never in production).

## License

Dual-licensed: the CLI and all engine code is Apache-2.0 (root `LICENSE`);
the default template and files generated from it are MIT-0
(`templates/default/TEMPLATE_LICENSE`; generated `.turutan.json` records
`templateLicense: "MIT-0"`).
