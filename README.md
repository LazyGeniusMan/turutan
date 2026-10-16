# turutan

[![License](https://img.shields.io/github/license/LazyGeniusMan/turutan)](./LICENSE) [![CI](https://github.com/LazyGeniusMan/turutan/actions/workflows/ci.yml/badge.svg)](https://github.com/LazyGeniusMan/turutan/actions/workflows/ci.yml) [![Go Version](https://img.shields.io/github/go-mod/go-version/LazyGeniusMan/turutan)](https://go.dev/)

Manage project-template lifecycle: scaffold new projects from versioned templates, then merge template updates back into them.

## Installation

```sh
go install github.com/LazyGeniusMan/turutan/cmd/turutan
```

## Quickstart

```sh
# Author a registry, scaffold a starter template, cut a release.
turutan registry init --author "Jane Doe <jane@example.com>" --name acme
turutan registry create foo  # recorded as created-but-unreleased: no version yet
turutan registry release foo minor fs
# first cut is 0.1.0 whatever the bump: manifest templates/foo/manifest/0.1.0.json,
# index releases prepended.

# Scaffold from a registry template URI at latest (default built-in URI).
# Use the full <registry-uri>/<template>[@version] form; bare names are rejected.
turutan init ./myapp --template oci://ghcr.io/acme/templates/foo --defaults
turutan init ./myapp --template oci://ghcr.io/acme/templates/foo@0.2.0 --defaults  # pinned release

cd myapp
turutan check          # exit 0 up-to-date, 2 update available
turutan diff           # exit 0 no drift, 2 drift found (add --ref <version> to preview a release)
turutan update         # merge the index-latest release (--ref <version> to pin, --conflict rej for .rej files)
turutan state sync     # re-render hand-edited state.yml inputs at the pinned release (no upstream check)
turutan state input    # re-answer every input, then re-render at the pinned release
turutan version        # version/commit/date plus license notice
```

## Remote authentication

Registry credentials come from the environment only, resolved by URI
scheme (one credential per scheme type), and are never logged.
S3-compatible: `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`,
`AWS_ENDPOINT_URL`, `AWS_REGION`. Git: `GITHUB_TOKEN`, `TURUTAN_SSH_KEY`,
`TURUTAN_SSH_PASSWORD`. OCI: `TURUTAN_OCI_USER`, `TURUTAN_OCI_PASSWORD`,
`TURUTAN_OCI_TOKEN`. HTTP(S): `TURUTAN_HTTP_USER`, `TURUTAN_HTTP_PASSWORD`,
`TURUTAN_HTTP_TOKEN`. The `fs` scheme is never authenticated.
Host-key (`known_hosts`) and TLS verification is strict by default;
`TURUTAN_INSECURE_SKIP_VERIFY=1` disables it with a loud warning
(never in production).

## License

Dual-licensed. The CLI and all engine code is Apache-2.0 (root `LICENSE`), the
default template is MIT-0 (`templates/default/LICENSE`).
