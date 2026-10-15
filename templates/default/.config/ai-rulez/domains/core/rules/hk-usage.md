---
type: Decision
title: Hk Usage
x-ai-rulez:
  kind: rule
  id: hk-usage
  metadata:
    priority: critical
---

This project use `turutan` default template. Only edit `hk` files in these paths to avoid update conflict unless the user explicitly requests a different one:
- `.config/hk/custom.pkl`

Always use `hk` builtins in the step if it's available, use custom step if not available. Prefer builtin, custom only when builtin can't express scope. Gates binary, check-only in CI.

Pre-commit (<3s, staged only): formatter staged write, fast linter staged subset, staged secret scan blocking redacted. Never build/test/network/integration/E2E.

Pre-push (30s-2min, changed files): heavier lint full-file, incremental type-check, affected hermetic unit <=60s, build fast-fail, branch-protection refusal, opt-in fast vuln max. Never integration/E2E/full-history/networked scans.

Commit-msg: hygiene only (format, length, prefix).

CI-safe vs local-only: `no-commit-to-branch` (branch-protection refusal) is `pre-push` hook ONLY, MUST NEVER appear in `check`/`fix` hooks used by CI. Pattern is `pre_push_ci` subset (same as `pre_push` minus `no-commit-to-branch`) for `check`/`fix`, full `pre_push` for `pre-push` hook.

Serialization: when `check`/`fix` merge `...pre_commit` + `...pre_push_ci` (or any merged hook), two steps invoking the same process-locked binary (notably `golangci-lint run`) MUST serialize via `depends` (e.g. `golangci-lint-full` depends `golangci-lint`); never rely on parallel execution; `-j 1` is not a fix.
