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
# manifest: templates/foo/manifest/0.2.0.json, index releases prepended.

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

## Dogfood: this repo authors the turutan registry

The registry is initialized and committed (`registry.json`, zero
releases): `templates/default/` is unreleased source until the first CI
dispatch cuts `0.1.0`. Template selection is explicit: `--template` flag
wins, then `TURUTAN_TEMPLATE`, then the user-config `template:`, defaulting
to `turutan/default@latest` via the OCI fallback (unresolvable until that
first publish).

Local fs dogfood never mutates the committed index. `turutan:release`
copies the committed empty `registry.json` plus `templates/` into the
gitignored overlay `.tmp/turutan-dogfood/`, inits the overlay registry
when missing, and cuts the fs release there (`same --force` on the
unindexed template resolves to `0.1.0`), so local commands exercise real
fs artifacts while the committed index stays empty.
Only the overlay outputs plus dogfood state stay gitignored:
`templates/*/manifest/*.json`,
`templates/*/releases/*.tar.gz`, `.config/turutan/state.yml` and
`.config/turutan/lock.json`. Hand-written seeds stay committed
(`dogfood-user.yml`, `dogfood-data.yml` — they hold no digests or
absolute paths). The `turutan:*` tasks are plain user commands
(`turutan <subcommand>`) with no dogfood wiring — that
lives in `.config/mise/config.custom.toml`, the hand-edit file that never
ships to template users (`turutan update` preserves it). It sets
`TURUTAN_CONFIG` (`.config/turutan/dogfood-user.yml`: `local` registry →
the overlay, as a relative path), pins
`TURUTAN_TEMPLATE=local/default@0.1.0`, and hosts the
`turutan:release` author task. On a fresh clone run the release once,
then everything works offline:

```sh
mise run turutan:release  # one-time overlay bootstrap on a fresh clone (see author loop)
mise run turutan:check    # uses local/default@0.1.0 from the overlay, no network
mise run turutan:diff     # drift vs the pinned archive (exit 2 when drifted)
```

The repo's own
`.config/turutan/state.yml` tracks `local/default@0.1.0` with the data in
`.config/turutan/dogfood-data.yml`, storing the pre-resolution relative
URI (`templates/default/releases/0.1.0.tar.gz`, resolved against the
overlay root) so the checkout moves freely between machines. Without
mise, export the same two variables (or run
`turutan registry add local .tmp/turutan-dogfood` once and export
`TURUTAN_TEMPLATE`).

Author loop (two commands): edit the tree under `templates/default/`,
rebuild the pinned overlay release in place, then re-check:

```sh
mise run turutan:release  # overlay copy, then registry release default same fs --force inside it:
                          # archive templates/default/releases/<v>.tar.gz,
                          # single-version manifest/<v>.json, index history updated — all under .tmp/;
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
The fs archive bytes and the OCI layer bytes come from the same
`BundleBytes` call over the same staged tree, so one digest covers both.

Simulate the next release outside the worktree: init a fresh temp root,
release there, and dogfood a scratch project from it (relative registry
addresses only, everything disposable):

```sh
sim=$(mktemp -d)
go build -o "$sim/turutan-bin" ./cmd/turutan
cp -r templates "$sim/templates"
(cd "$sim" && ./turutan-bin registry init --name turutan --author "Andriyansyah Nurrachman <git@yansyah.my.id> (https://yansyah.my.id)")
printf -- '---\ntemplate: local/default@0.1.0\nregistries:\n  local: .\n' > "$sim/reg.yml"
(cd "$sim" && TURUTAN_CONFIG="$sim/reg.yml" ./turutan-bin registry release default minor fs --force)
(cd "$sim" && TURUTAN_CONFIG="$sim/reg.yml" TURUTAN_TEMPLATE=local/default@0.2.0 \
  ./turutan-bin init ./proj --data-file "$PWD/.config/turutan/dogfood-data.yml" --defaults --non-interactive)
printf -- '---\ntemplate: local/default@0.2.0\nregistries:\n  local: ..\n' > "$sim/proj.yml"
(cd "$sim/proj" && TURUTAN_CONFIG="$sim/proj.yml" TURUTAN_TEMPLATE=local/default@0.2.0 "$sim/turutan-bin" check)
rm -rf "$sim"
```

To dogfood a bumped release for real, run the simulation above, then
bump the `TURUTAN_TEMPLATE` pin in `.config/mise/config.custom.toml`,
`dogfood-user.yml` and `.config/turutan/state.yml` (re-bootstrap via
`turutan init` into a temp dir with
`--data-file .config/turutan/dogfood-data.yml`, then copy the new
state/lock/base back). Users stay on OCI:
`turutan init ./myapp --template turutan/default@latest --defaults` needs
no config. Publishing is a CI job (`.github/workflows/templates-publish.yml`,
manual dispatch with a `major|minor|patch` input): on the committed empty
index the first dispatch cuts `0.1.0` (unindexed templates always start at
`0.1.0`, whatever the bump), pushes `default:<version>` to GHCR with
`--uri-override`, and commits the bumped `registry.json` (tracked) plus
force-adds the versioned manifest back (manifests stay gitignored until
force-added). The first real release happens via that dispatch. The exact
manual first-publish command (no creds in a sandbox) is `turutan registry
release default patch oci --uri-override
oci://ghcr.io/lazygeniusman/turutan/templates/default` with
`TURUTAN_OCI_USER` + `TURUTAN_OCI_PASSWORD` (or `TURUTAN_OCI_TOKEN`) set.

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
this repo roots it (committed empty `registry.json`, unreleased
`templates/default` source until the first publish cuts `0.1.0`); author
with `registry init/create/release`.
