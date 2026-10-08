# turutan template

This is a project template that include many profiles. Each profiles is preconfigured to install toolchains and git hooks, it will also setup relevant AI Agents skills, rules, and MCPs if `ai` profile is enabled.

## Prerequisite

* [`mise`](https://mise.jdx.dev) is installed and `mise doctor` command return no problems found.

## Profiles

Configure active profiles in [`.config/miserc.toml`](.config/miserc.toml). Available profiles are:

### Universal

* `core`

    [`mise`](https://mise.jdx.dev) as toolchain, dependencies, and task runner manager; [`hk`](https://hk.jdx.dev) as git hooks manager that configure linter, formatter, etc; [`pitchfork`](https://pitchfork.jdx.dev) as background process manager; [`fnox`](https://fnox.jdx.dev) as secret manager. [Proton Pass](https://proton.me/pass) is preinstalled and preconfigured as secret provider.

* `ai`

    [`ai-rulez`](https://github.com/Goldziher/ai-rulez) as centralized AI Agents configuration, rules, skill, and mcp manager; [`basemind`](https://github.com/Goldziher/basemind) as codebase context indexer. [`pi`](https://pi.dev) and [T3 Code](https://t3.codes) Web UI is preconfigured as AI Agent; add other AI Agents by adding it to [`.config/mise/config.ai.toml`](.config/mise/config.ai.toml).

### Programming Languages

* `c`

    C standard toolchains.

* `cpp`

    C++ standard toolchains.

* `csharp`

    C# standard toolchains.

* `elixir`

    Elixir standard toolchains.

* `go`

    Golang standard toolchains.

* `java`

    Java standard toolchains.

* `php`

    PHP standard toolchains.

* `python`

    [`uv`](https://docs.astral.sh/uv) as package manager; [`ruff`](https://docs.astral.sh/ruff) as linter and [`ty`](https://docs.astral.sh/ty) as type checker.

* `r`

    R standard toolchains.

* `ruby`

    Ruby standard toolchains.

* `rust`

    Rust standard toolchains.

* `rust-polygot`

    [`alef`](https://github.com/xberg-io/alef) as Rust FFI codegen for other languanges.

* `typescript`

    [`Vite+`](https://viteplus.dev) as unified toolchains.

### Platforms

* `cloudflare`

    Cloudflare toolchains

* `docker`

    Docker Container toolchains

* `kubernetes`

    Kubernetes toolchains

* `github`

    Github toolchains

## Setup

### First Time
1. Install project dependencies
```sh
mise trust && mise install
```
2. Configure dependencies
```sh
mise run dev-setup:**
```

### Development
```sh
mise run dev:**
```

### Polygot FFI Binding
```
mise bootstrap package i
```
