---
type: Decision
title: Go Skill Usage
x-ai-rulez:
  kind: rule
  id: go-skill-usage
  domain: go
  metadata:
    priority: critical
    activation: glob
    paths:
      - '**/*.go'
      - '**/go.mod'
      - '**/go.sum'
---

When working on golang project, always use `golang-how-to` skill before doing anything and when delegating task to other agents and subagents.
