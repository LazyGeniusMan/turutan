---
type: Concept
title: Typescript Toolchains
x-ai-rulez:
  kind: context
  id: typescript-toolchains
  metadata:
    priority: critical
---

This project is using [Vite+](https://github.com/voidzero-dev/vite-plus), a unified toolchain built on top of Vite, Rolldown, Vitest, tsdown, Oxlint, Oxfmt, and Vite Task. Vite+ wraps runtime management, package management, and frontend tooling in a single global CLI called `vp`. Vite+ is distinct from Vite, and it invokes Vite through `vp dev` and `vp build`. Run `vp help` to print a list of commands and `vp <command> --help` for information about a specific command.

Docs are available at `node_modules/vite-plus/docs`
