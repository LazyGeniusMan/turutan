---
type: Decision
title: Skill Usage
x-ai-rulez:
  kind: rule
  id: skill-usage
  metadata:
    priority: critical
---

Always try to find appropiate skill before doing any task and suggest skill when delegating task to other agents or subagents.

Some skills are served over MCP and are not listed in this session. Call `find_skill` with a short description of the task, then `load_skill` with the chosen name, on the `ai-rulez` MCP server. If that server is unavailable, read `.config/ai-rulez/**/skills/<skill-name>/SKILL.md` instead.
