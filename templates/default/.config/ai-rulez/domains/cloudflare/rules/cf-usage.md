---
type: Decision
title: Viteplus Usage
x-ai-rulez:
  kind: rule
  id: viteplus-usage
  domain: typescript
  metadata:
    priority: critical
    activation: glob
    paths:
      - '**/cloudflare.config.ts'
---

Never use `wrangler` command, always use `cf` command.
