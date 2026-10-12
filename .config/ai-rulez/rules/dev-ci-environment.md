---
type: Decision
title: Dev Ci Environment
x-ai-rulez:
  kind: rule
  id: dev-ci-environment
  metadata:
    priority: critical
---

Development and CI must use the exact same environment and workflow. `hk` must be source of truth for workflow because it can be configured as enforcement locally via git hooks, it can integrate with git for partial check based on diff on matched globs, and the step can be configured to be executed conditionally based on profile. `mise` task must call `hk` command that can be used manually for local dev workflow and used in CI workflow.

Parity: same tool/version/config/command locally and CI. Layers differ only in scope (staged vs changed vs full), never in correctness. Strongest form: CI executes same hk definitions in whole-tree mode.

Check-vs-fix: auto-fix deterministic locally, check-only never-rewrite in CI. Secrets rotate, never autofix.

Budgets: pre-commit <3s, pre-push 30s-2min, PR <10min. Hooks bypassable, never substitute CI; CI re-verifies.
