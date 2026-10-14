---
type: Decision
title: Go Toolchains
x-ai-rulez:
  kind: rule
  id: go-toolchains
  domain: go
  metadata:
    priority: critical
    activation: glob
    paths:
      - '**/*.go'
      - '**/go.mod'
      - '**/go.sum'
---

When working on golang project, always use [`golangci-lint`](https://github.com/golangci/golangci-lint) to configure linter, formatter, etc if it's available and configurable in `golangci-lint`config, do not manage separate command and configuration.

`run` uses `./...` package mode (see global toolchains-usage); concurrent `run` invocations on one workspace collide (`parallel golangci-lint is running` lock) — serialize via hk `depends`, never parallelize. `fmt` may use `{{files}}` and is lock-free.
