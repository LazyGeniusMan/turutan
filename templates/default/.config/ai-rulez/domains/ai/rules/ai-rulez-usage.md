---
type: Decision
title: Ai-rulez Usage
x-ai-rulez:
  kind: rule
  id: ai-rulez-usage
  metadata:
    priority: critical
---

This project use `turutan` default template. Only edit `ai-rulez` files in these paths to avoid update conflict unless the user explicitly requests a different one:
- `.config/ai-rulez/config.custom.toml`
- `.config/ai-rulez/domains/custom/**`
