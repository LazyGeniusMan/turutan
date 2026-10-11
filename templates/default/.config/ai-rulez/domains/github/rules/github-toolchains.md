---
type: Decision
title: Github Toolchains
x-ai-rulez:
  kind: rule
  id: github-toolchains
  domain: github
  metadata:
    priority: critical
    activation: glob
    paths:
      - '.github/**'
---

When working on Github action workflow, always use [`jactionlint`](https://github.com/jdx/jactionlint) to configure linter.
