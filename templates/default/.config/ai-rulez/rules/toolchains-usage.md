---
type: Decision
title: Toolchains Usage
x-ai-rulez:
  kind: rule
  id: toolchains-usage
  metadata:
    priority: critical
---

This project toolchains is using profiles concept. The active profile source of truth is on `.config/miserc.toml` `env`. The value is being used in `mise` as `MISE_ENV`, in `hk` as `HK_PROFILE`, and `ai-rulez` as `profiles`. Other toolchain must use this concept too if supported.

`.config/miserc.toml` is convention-only (mise v2026.10.x never reads it); effective profiles come from `.config/mise/config*.toml` + MISE_ENV unset-means-all. `HK_PROFILE` derives via `{{ mise_env | default(value=[]) | join(sep=',') }}` so MISE_ENV unset runs full scope. Never pin empty string in CI; future `config.<new>.toml` is auto-included when unset.

golangci-lint maps to `go` profile. Scope is hk-driven via `{{files}}`: staged locally, branch diff in PR CI (`hk check --from-ref origin/main --to-ref HEAD`), full on main/scheduled (`hk check --all`). Config canonical path `.config/golangci-lint/.golangci.yml`.

golangci-lint `run` MUST use `./...` package mode, never `{{files}}`: a multi-package file list trips `named files must all be in one directory` typecheck failure. Config MUST keep `issues.new-from-merge-base: main` so main/full scope gates only new findings. Exception to `{{files}}` scoping: `run` is package-mode, `fmt` may still use `--diff` with `{{files}}`.
