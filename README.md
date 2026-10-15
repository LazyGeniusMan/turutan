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

# Author a registry, scaffold a starter template, cut a release.
turutan registry init --author "Jane Doe <jane@example.com>" --name acme
turutan registry create foo
turutan registry release foo minor fs
# manifest: templates/foo/manifest/0.2.0.yml, index releases prepended.

# Point a name at the registry (fs/http/s3/git/oci URIs accepted).
turutan registry add acme ./registry-dir

# Scaffold from a registry template (default turutan/default at latest).
turutan init ./myapp --template acme/foo --defaults
turutan init ./myapp --template acme/foo@0.2.0 --defaults  # pinned release

cd myapp
turutan check   # exit 0 up-to-date, 2 update available
turutan diff           # exit 0 no drift, 2 drift found (add --ref <version> to preview a release)
turutan update         # merge the index-latest release (--ref <version> to pin, --conflict rej for .rej files)
turutan state sync     # re-render hand-edited state.yml inputs at the pinned release (no upstream check)
turutan state input    # re-answer every input, then re-render at the pinned release
turutan version        # version/commit/date plus license notice
```

## Dogfood: this repo is the turutan registry

The repo root is the `turutan` registry root for authoring:
`registry.json` plus `templates/default` with fs-relative release URIs
(`templates/default/releases/<v>.tar.gz`). The published OCI artifact
(`oci://ghcr.io/lazygeniusman/turutan/templates`) carries OCI URIs for
users. Template selection is explicit: `--template` flag wins, then
`TURUTAN_TEMPLATE`, then the user-config `template:`, defaulting to
`turutan/default@latest` via the OCI fallback.

All generated registry artifacts are gitignored and rebuilt on demand:
`registry.json`, `templates/*/manifest/`, `templates/*/releases/`,
`.config/turutan/state.yml` and `.config/turutan/lock.json`. Only
hand-written seeds stay committed (`dogfood-user.yml`,
`dogfood-data.yml` — they hold no digests or absolute paths). Every
`turutan:*` task depends on `turutan:release`, so a fresh clone works
offline with one command:

```sh
mise run turutan:check   # rebuilds artifacts, uses local/default@0.1.0, no network
mise run turutan:diff    # drift vs the pinned archive (exit 2 when drifted)
```

The mise `turutan:*` tasks point `TURUTAN_CONFIG` at
`.config/turutan/dogfood-user.yml` (`local` registry → repo root) and pin
`TURUTAN_TEMPLATE=local/default@0.1.0`. The repo's own
`.config/turutan/state.yml` tracks `local/default@0.1.0` with the data in
`.config/turutan/dogfood-data.yml`, storing the pre-resolution relative
URI (`templates/default/releases/0.1.0.tar.gz`) so the checkout moves
freely between machines. Without mise, export the same two variables (or
run `turutan registry add local <repo-root>` once and export
`TURUTAN_TEMPLATE`).

Author loop (two commands): edit the tree under `templates/default/`,
rebuild the pinned release in place, then re-check:

```sh
mise run turutan:release  # registry release default same fs --force:
                          # archive templates/default/releases/<v>.tar.gz,
                          # single-version manifest/<v>.json, index history updated;
                          # also restores missing dogfood state/lock/base on fresh clones
TURUTAN_TEMPLATE=local/default@<v> turutan diff --ref <v>
```

`registry release` bumps with `major|minor|patch` (new templates start at
`0.1.0`) and rebuilds in place with `same --force` — no bump, just a
fresh archive, manifest and index entry for the pinned version. `--force`
is the rebuild switch: without it the command refuses to overwrite the
versioned manifest, the release archive, or an already-listed version.
The index entry keeps the latest manifest fields plus a newest-first
`releases` history; versioned manifest files hold one version each.

Bump the `TURUTAN_TEMPLATE` pin in the mise tasks, `dogfood-user.yml` and
`.config/turutan/state.yml` (re-bootstrap via `turutan init` into a temp
dir with `--data-file .config/turutan/dogfood-data.yml`, then copy the new
state/lock/base back) to dogfood the new release. Users stay on OCI:
`turutan init ./myapp --template turutan/default@latest --defaults` needs
no config. To publish, cut an OCI release (no push happens here):
`turutan registry release default <major|minor|patch> oci --uri-override
oci://ghcr.io/lazygeniusman/turutan/templates/default`.

## Remote authentication

Registry credentials come from the environment only and are never
logged. Per-registry overrides use `TURUTAN_REGISTRY_<NAME>_USER`,
`_PASS`, `_TOKEN` and `_ENDPOINT`; scheme defaults cover S3-compatible
(`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_ENDPOINT_URL`,
`AWS_REGION`), git (`GITHUB_TOKEN`, `TURUTAN_SSH_KEY`) and OCI
(`TURUTAN_OCI_USER`, `TURUTAN_OCI_PASSWORD`, `TURUTAN_OCI_TOKEN`).
Host-key (`known_hosts`) and TLS verification is strict by default;
`TURUTAN_INSECURE_SKIP_VERIFY=1` disables it with a loud warning
(never in production).

## License

Dual-licensed: the CLI and all engine code is Apache-2.0 (root `LICENSE`);
template content carries its own license: each release records the
template `.turutan.yml` license, and generated
`.config/turutan/state.yml` records it as `templateLicense`
(e.g. `"MIT-0"`). The registry is the source of truth for templates —
this repo roots it (`registry.json`, `templates/default`); author with
`registry init/create/release`.
