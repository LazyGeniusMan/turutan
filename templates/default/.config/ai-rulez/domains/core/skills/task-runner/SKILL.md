---
type: Playbook
title: Task Runner
description: Use `mise` instead of raw build/test/lint commands with the standard target names setup, build, test, lint, format, bench. Load before running a build, test, lint or format command in an unfamiliar repository.
x-ai-rulez:
  kind: skill
  id: task-runner
  domain: core
  metadata:
    name: task-runner
---

- Prefer `mise run <target>` over the raw toolchain command, mise task carries the flags and env the raw command omits.
- `mise task` enumerates the available targets.
- Standard target names: `setup`, `build`, `test`, `lint`, `format`, `bench`. Follow them when adding targets.
- Commit lock files so builds are reproducible.
