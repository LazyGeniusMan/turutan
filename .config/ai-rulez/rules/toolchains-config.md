---
type: Decision
title: Toolchains Config
x-ai-rulez:
  kind: rule
  id: toolchains-config
  metadata:
    priority: critical
---

Always use `.config` folder to store toolchains configuration if the toolchain support it by default or configurable via environment variable and CLI arg. Storing toolchain in project root directory is highly discourage.
