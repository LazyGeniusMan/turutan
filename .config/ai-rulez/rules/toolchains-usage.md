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

golangci-lint maps to `go` profile. Scope: diff on PR/pre-push via `--new-from-merge-base`/`--new-from-rev` + `--whole-files`, full on main/scheduled via bare run. Config canonical path `.config/golangci-lint/.golangci.yml`.
