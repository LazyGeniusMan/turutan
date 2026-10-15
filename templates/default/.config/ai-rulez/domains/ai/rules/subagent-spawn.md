---
type: Decision
title: Subagent Spawn
x-ai-rulez:
  kind: rule
  id: subagent-spawn
  metadata:
    priority: critical
---

Always spawn subagents without setting the provider, model and thinking effort so it's defaulted to the subagents configuration. If provider, model and thinking effort is mandatory, set it to the same provider, model and thinking effort as the currently used unless the user explicitly requests a different one.
