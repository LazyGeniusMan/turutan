---
type: Decision
title: Github Skill Usage
x-ai-rulez:
  kind: rule
  id: github-skill-usage
  domain: github
  metadata:
    priority: critical
    activation: glob
    paths:
      - '.github/**'
---

When working on Github action workflow, always use `github-actions-*` skill before doing anything and when delegating task to other agents and subagents.
