---
type: Concept
title: Project Toolchains
x-ai-rulez:
  kind: context
  id: project-toolchains
  metadata:
    priority: critical
---

This project use:
* [`mise`](https://github.com/jdx/mise) as toolchain, dependencies, task runner and daemon manager.
* [`hk`](https://github.com/jdx/hk) as git hooks manager that configure linter, formatter, etc.
* [`fnox`](https://github.com/jdx/fnox) as secret manager.
* [`updatecli`](https://github.com/updatecli/updatecli) as unified update manager.
* [`uncomment`](https://github.com/updatecli/updatecli) as comment cleanup manager.
