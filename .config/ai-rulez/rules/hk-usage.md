---
priority: critical
---

Always use `hk` builtins in the step if it's available, use custom step if not available. Separate `hk` steps into pre-commit and pre-push. Formatter, linter, unit test, integration test and secret leaks check must be configured at pre-commit. Security check, conformance check, build, and e2e test must be configured for pre push.
