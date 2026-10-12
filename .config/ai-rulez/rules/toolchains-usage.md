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

golangci-lint maps to `go` profile. Scope is hk-driven via `{{files}}`: staged locally, branch diff in PR CI (`hk check --pr`), full on main/scheduled (`hk check --all`). Config canonical path `.config/golangci-lint/.golangci.yml`.
