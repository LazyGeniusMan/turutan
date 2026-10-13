---
type: Decision
title: Mise Usage
x-ai-rulez:
  kind: rule
  id: mise-usage
  metadata:
    priority: critical
---

This project use `turutan` default template. Only edit `mise` files in these paths to avoid update conflict unless the user explicitly requests a different one:
- `.config/mise/config.custom.toml`
