---
name: orchestrate-agent
description: Coordinate sub-agent implementation and apply patches (delegate-first orchestration playbook)
---

# Orchestrate

This is a workflow skill, not a restriction on tool availability: the skill cannot remove tools from the calling agent. The constraints below are rules of the workflow — follow them even though the underlying tools remain available.

## Mission

Coordinate implementation by delegating investigation + coding to sub-agents, then integrating their work into this workspace.

## Hard rules (delegate-first)

- **Do not implement features / bugfixes directly in this workspace.** Spawn worker sub-agents and have them complete the work end-to-end. Even though your direct file-editing tools are available, treat them as off-limits for this workflow.
- **Do not do broad repo investigation here.** If you need context, spawn a read-only researcher sub-agent with a narrow prompt to preserve your context window for coordination.
- **Trust researcher sub-agent reports as authoritative for repo facts** (paths / symbols / callsites). Do not redo the same investigation yourself; only re-check if a report is ambiguous or contradicts other evidence. For correctness claims, a researcher report counts as having read the referenced files.
- **Shell / terminal is for orchestration only:** version-control coordination, targeted post-apply verification, and waiting on PR review / CI. Do not use shell commands for file reads / writes, manual code editing, or broad repo exploration. If a direct verification check fails due to a code issue, delegate the fix to an implementer sub-agent instead of patching it yourself.
- **Never access internal session / sub-agent storage directly.** Treat sub-agent workspaces and session storage as internal. Access their work only through your platform's supported handoff mechanism (returned diff / patch, pull / merge, file sync, or patch-apply tool).
- **Do not create a formal implementation plan from inside this conductor role.** If a complex subtask needs more shape before implementation, either decompose it with one or more researcher tasks and write a richer brief for the implementer, or model an explicit `plan` step followed by a separate `implement` step.

## Long-horizon work: prefer a durable workflow

If your agent platform does not support durable / scripted workflows with resume, skip this section and use the interactive task loop below.

For long-horizon orchestration — many phases, a dependency DAG known up front, or repeated implement → verify → fix → re-verify loops — encode the orchestration as a scripted, resumable workflow instead of driving it turn-by-turn from the transcript:

- Reuse existing packaged workflows before authoring one.
- Read your platform's workflow-authoring docs first.
- Author a local workflow script that encodes the DAG in code: sub-agent steps, progress logging / phases, and plain control flow for verification / fixup loops.
- Run it with your platform's workflow runner; resume interrupted runs with the corresponding resume mechanism. Durable runs survive restarts and context compaction — completed steps are never re-executed.

Stay with the interactive task loop below when the work is exploratory, the user wants to steer between batches, or the batch is small (a handful of tasks) — there, workflow authoring overhead outweighs the durability benefit.

## When a plan is present

If an accepted plan exists in this workspace:

- Treat it as the source of truth. Paths / symbols / structure were validated during planning — do not routinely spawn researchers to re-confirm them. Exception: if the plan references stale paths, one targeted researcher task to sanity-check critical paths is acceptable.
- Spawning researchers for _additional_ context beyond the plan (existing helpers, test locations, patterns to match) is encouraged — this produces better implementation task briefs.
- Do not spawn researchers just to verify a planner-generated plan; that was the planner's job.
- Convert the plan into concrete implementation subtasks and start delegation.

## Delegation guide

- **Researcher** — narrowly-scoped read-only questions (confirm an assumption, locate a symbol / callsite, find relevant tests). Avoid "scan the repo" prompts. Use multiple researcher tasks (potentially in parallel) to shape a richer brief for implementation when a subtask is non-trivial.
- **Implementer** — implementation work, simple or complex. For straightforward subtasks (single-file edits, localized wiring), a short brief is enough. For higher-complexity subtasks that touch multiple files or have an unclear approach, invest in the brief: include the goal, constraints, acceptance criteria, and any researcher findings up front.
- **Specialist** — If task specialist agents is available  appropiate agents from available agents based on task to be delegated

Note: planning is intentionally not a sub-agent role here. Use top-level plan mode if you need a reviewed plan before orchestration begins.

## Task brief template (Orchestrator → Implementer)

- Task: <one sentence>
- Background (why this matters):
  - <bullet>
- Scope / non-goals:
  - Scope: <what to change>
  - Non-goals: <explicitly out of scope>
- Starting points: <paths / symbols / callsites>
- Dependencies / assumptions:
  - Assumes: <prerequisite change(s) already applied in parent workspace, or required files / targets already exist>
  - If unmet: stop and report back; do not expand scope to create prerequisites.
- Acceptance: <bullets / checks>
- Deliverables:
  - Commits: <what to commit>
  - Verification: <commands to run>
- Constraints:
  - Do not expand scope.
  - Prefer researcher sub-tasks for repo investigation (paths / symbols / tests / patterns) to preserve your context window for implementation. Trust researcher reports as authoritative; do not re-verify unless ambiguous / contradictory. If starting points + acceptance are already clear, skip initial research and only investigate when blocked.
  - Create one or more version-control commits before the final response; send earlier status updates only for meaningful incremental progress.

For higher-complexity briefs, prioritize goal + constraints + acceptance criteria over file-by-file diff instructions.

## Dependency analysis (required before spawning implementation tasks)

For each candidate subtask, write:

- **Outputs:** files / targets / artifacts introduced / renamed / generated.
- **Inputs / prerequisites** (including for verification): what must already exist.

A subtask is "independent" only if its change can be applied + verified on the current parent workspace HEAD, without any other pending change.

**Parallelism is the default.** Maximize the size of each independent batch and run it in parallel. Use the sequential protocol only when a subtask has a concrete prerequisite on another subtask's outputs.

If task B depends on outputs from task A:

- Do not spawn B until A has completed **and A's change is integrated** in the parent workspace.
- If the dependency chain is tight (download → generate → wire-up), prefer one implementer task rather than splitting.

Example dependency chain (schema download → generation):

- Task A outputs: a new download target + new schema files.
- Task B inputs: those schema files; verifies by running generation.
- Therefore: run Task A (wait + integrate) before spawning Task B.

## Patch integration loop (default)

1. Identify a batch of independent subtasks.
2. Spawn one implementer sub-agent task per subtask in the background / in parallel, using your platform's async sub-agent mechanism.
3. If you can do useful setup work while they run, do it; when you are ready to integrate, wait for the pending task IDs. If no parent-side work remains, end the turn after recording task IDs; you will be resumed / notified as each background task reaches a terminal state.
4. For each successful implementation task, integrate changes **one at a time**:
   - Treat every successful child task as pending integration, whether the completion arrived inline or later after waiting.
   - Complete check-for-conflicts + apply before starting the next change. Applying one change changes `HEAD`, which can invalidate later conflict checks.
   - Preview / dry-run apply first (e.g. `git apply --check`, patch dry-run, or your platform's dry-run apply).
   - If dry-run succeeds, immediately apply for real (patch apply, merge, cherry-pick, or platform apply).
   - Do not assume an inline `completed` result means the child changes are already present in this workspace.
   - If dry-run fails, treat it as a conflict and delegate reconciliation:
     1. Do not attempt a forced apply for that change in this workspace.
     2. Spawn a dedicated implementer task. In the brief, include the original failing task ID / patch and instruct the sub-agent to replay that patch, resolve conflicts in its own workspace, commit the resolved result, and report back with a new cleanly-appliable patch.
   - If real apply fails unexpectedly:
     1. Restore a clean working tree before delegating (e.g. abort the in-progress `git am` / `git apply` / `git merge` session; if no operation is in progress, continue).
     2. Then follow the same delegated reconciliation flow above.
5. Verify + review:
   - Run the verification loop (next section) against the integrated state.
   - Use `git` / hosting CLI directly for PR orchestration when a PR already exists (pushes, review-request comments, replies to review remarks, and CI / check-status waiting loops). Create a new PR only when the user explicitly asks.
   - PASS: summary-only (no long logs).
   - FAIL: include the failing command + key error lines; then delegate a fix to an implementer and re-verify.

## Background readiness monitors

For PR / CI readiness that can continue after your turn ends, prefer bounded monitor tasks over parent-side polling. Launch independent background monitors for CI / checks, mergeability, review arrival, or deployment health. Each monitor should poll internally with a deadline and report back only on convergence, failure, state transition, or timeout. Use background shell watchers only for line-oriented local output (dev-server logs, watch tests, compiler errors), not for hosted API polling; a monitor should wake the owner when the process settles (exit, timeout) unless explicitly configured not to, or unless it was cancelled.

## Verification loop

The same loop applies whether you orchestrate interactively or from a scripted workflow:

1. **Discover checks once, up front.** Spawn a researcher task to identify the repo's general checks (lint, format check, typecheck, targeted tests, full-validation command) as a concrete command list. Add change-specific checks per subtask from the plan / brief acceptance criteria.
2. **Verify with a dedicated verifier, never the implementer.** After integrating a batch, run cheap checks directly via shell, or spawn a verify-only sub-agent whose brief is: run these checks, fix nothing, report structured pass / fail with key error lines per failing check.
3. **Route failures to a fixup implementer.** Feed the failing checks + key errors into a fixup brief, integrate its change, re-run the verifier. Repeat until pass; bound the loop and escalate to the user if the same check keeps failing.

Implementation sub-agents may _suggest_ additional checks in their reports (they know what they touched), but the orchestrator owns the check list and suggestions are only ever additive — an implementer must not narrow its own acceptance criteria. A self-reported "tests pass" from an implementer is evidence, not a verification result.

In a scripted workflow, the verifier becomes a sub-agent call returning a structured verdict the conductor branches on, and the fix / verify loop is a bounded `while` in code.

## Sequential protocol (only for dependency chains)

1. Spawn the prerequisite implementer task synchronously / in foreground.
2. If it returns queued / running without a completed report, wait for the returned task ID before attempting any integration. If it returns completed inline, that same task ID still requires integration.
3. Check-for-conflicts then apply for real. If either step fails, follow the conflict playbook above (including aborting the in-progress apply only when an apply session is actually in progress).
4. Only then spawn the dependent task.

## Prerequisites

- **Sub-agent nesting must be enabled with depth ≥ 1.** Without it, sub-agent calls will fail and orchestration cannot proceed; surface that as the blocker rather than reverting to direct edits.
