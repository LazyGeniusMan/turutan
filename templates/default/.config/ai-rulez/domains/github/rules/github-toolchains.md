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

Pin `runs-on` to fixed label (e.g. `ubuntu-24.04`, never `ubuntu-latest` alias → mutable-runner-label); quote YAML `on` as `"on"` (ryl truthy); invoke `ryl check`, never bare `ryl <paths>` (deprecated). `templates/default/.github/workflows` copies inherit same constraints.
