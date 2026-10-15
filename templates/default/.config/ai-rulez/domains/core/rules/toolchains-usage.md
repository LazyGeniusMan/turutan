---
type: Decision
title: Toolchains Usage
x-ai-rulez:
  kind: rule
  id: toolchains-usage
  metadata:
    priority: critical
---

This project toolchains is using profiles concept. The active profile source of truth is on `.config/miserc.toml` `env`. The value is being used in `mise` as `MISE_ENV`, in `hk` as `HK_PROFILE`, and `ai-rulez` as `AI_RULEZ_PROFILE`. Other toolchain must use this concept too if supported.
