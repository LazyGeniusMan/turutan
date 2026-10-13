---
type: Decision
title: Subagent Model Pinning
x-ai-rulez:
  kind: rule
  id: subagent-model-pinning
  metadata:
    priority: critical
---

Always spawn subagents with the same model and thinking effort as the current thread; never pass explicit model/effort/provider unless the user explicitly requests a different one. When the platform defaults to inheritance (T3 delegate_task without target, pi subagents without model args), leave those fields unset. omit_agent_fields already ensures generated agents inherit, so do not reintroduce explicit model/effort there.
