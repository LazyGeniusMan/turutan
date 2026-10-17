---
type: Concept
title: Core Toolchains
x-ai-rulez:
  kind: context
  id: core-toolchains
  metadata:
    priority: critical
---

This project is using:
* [`mise`](https://github.com/jdx/mise) as dependencies, task runner and daemon manager.
* [`hk`](https://github.com/jdx/hk) as git hooks manager that configure linter, formatter, etc.
* [`fnox`](https://github.com/jdx/fnox) as secret manager.
* [`updatecli`](https://github.com/updatecli/updatecli) as unified update manager.
