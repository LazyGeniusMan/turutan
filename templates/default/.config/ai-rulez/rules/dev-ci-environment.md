---
priority: critical
---

Development and CI must use the exact same environment and workflow. `hk` must be source of truth for workflow because it can be configured as enforcement locally via git hooks, it can integrate with git for partial check based on diff on matched globs, and the step can be configured to be executed conditionally based on profile. `mise` task must call `hk` command that can be used manually for local dev workflow and used in CI workflow.
