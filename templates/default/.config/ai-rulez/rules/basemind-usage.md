---
priority: critical
---

basemind is this repo's indexed context layer. Prefer it BEFORE grep, before reading files to find structure, and before naked `git` — it's the default, not a preference. basemind returns paths, lines, and signatures at a fraction of the tokens of reading source.

#### Routing

| Reach for | Instead of |
|---|---|
| `search_symbols` / `goto_definition` / `find_references` / `find_callers` / `find_implementations` / `call_graph` / `workspace_grep` | `grep` / `rg` / opening files to find a symbol |
| `outline` / `architecture_map` | reading whole files to learn their shape |
| `recent_changes` / `blame_symbol` / `commits_touching` / `diff_file` | `git log` / `git blame` / `git diff` |
| `find_files` (fuzzy path search) | `find` / `fd` / `ls -R` to locate a file by name |
| `agents` (modes `post` / `inbox` / `thread_list`) | assuming you're the only agent in the repo |
| `workspace` (modes `workspaces` / `worktrees` / `claim`) | editing a worktree another session may already own |
| `memory` mode `documents` / `web` (modes `scrape` / `crawl` / `map`) | manually reading PDFs / docs or ad-hoc fetching |
| semantic code search over the index | keyword-only guessing at where a concept lives |

#### Red flags — stop and re-route

- About to `grep` / `rg`? → `workspace_grep`.
- About to open a file just to find a symbol? → `outline` / `search_symbols`.
- About to `git log` / `git blame`? → `recent_changes` / `blame_symbol`.
- Already mapped a file with basemind? Don't re-read it.
