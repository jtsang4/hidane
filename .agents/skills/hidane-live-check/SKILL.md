---
name: hidane-live-check
description: Verify a hidane change against the real local claude, codex and pi CLIs in a throwaway HIDANE_HOME. Use after changing an agent CLI driver (internal/agentcli), the guard (internal/guard), a charter or the context a role is given (internal/agents), or anything else where the fake CLIs cannot tell you how a real model behaves. Spends real model tokens.
---

# Live check on the real agent CLIs

The fakes (`cmd/fakeagent`) prove protocol handling; they cannot show what a
real model does with what hidane hands it. Every problem below was invisible to
the fake-backed tests and found by this procedure:

- a Primary told it was a coding agent claimed file edits it never made;
- a Primary wrote its own cwd into a brief, and a worker then wrote into
  hidane's data directory;
- a broken `POLICY.json` silently disabled every rule;
- a refused compound command was reported as "half done".

## Run it

`scripts/live-check.sh` builds the headless binary, makes a fresh
`HIDANE_HOME` under `/tmp`, writes `settings.json` and an optional policy file
**with a JSON encoder** (shell `echo` mangles backslashes such as `\.` and
produces a policy file that is silently invalid), runs one `hidane chat` task,
and prints what to judge.

```sh
# worker on each CLI, cheap reasoning roles, a rule that must refuse one write
.agents/skills/hidane-live-check/scripts/live-check.sh --worker claude --policy 'forbidden\.txt' \
  "在工作区里创建 hello.txt，内容是 hi；再创建 forbidden.txt，内容是 x。最后告诉我结果。"
.agents/skills/hidane-live-check/scripts/live-check.sh --worker codex --policy 'forbidden\.txt' "…same task…"
.agents/skills/hidane-live-check/scripts/live-check.sh --worker pi    --policy 'forbidden\.txt' "…same task…"

# every role on one CLI
.agents/skills/hidane-live-check/scripts/live-check.sh --all codex "在工作区里创建 notes.md，写一句问候语，然后告诉我写了什么。"
```

Options: `--worker <cli>` (reasoning roles stay on claude with `--model`,
default `haiku`), `--all <cli>` (every role on one CLI), `--model <id>`,
`--policy <regex>`. Run several in parallel with `&` — each gets its own home.
For a single round trip per role, `bin/hidane-nogui model --ping` is cheaper.

## Judge — don't stop at exit code 0

The script prints the decisions, executions, refusals and replies, and every
file the workers produced. Check all of these:

1. **Routing**: a task became a `work_item.created`; small talk did not. A
   `reply` effect that claims work was done, with no work item, is a fabrication.
2. **Briefs carry no paths of hidane's own** (the role directory, `HIDANE_HOME`).
3. **Artifacts are inside `workspaces/<wi>/`**: nothing next to `settings.json`
   or under `runtime/`.
4. **Policy**: the refused file does not exist, and there is a `policy.blocked`
   naming the rule.
5. **The final reply matches the files on disk**, not just the worker's prose.
6. No `agent.error`, and nothing left `queued`/`running` (`bin/hidane-nogui items`).

Report the evidence you looked at (event lines, `ls` output) with the verdict.
Delete the `/tmp/hidane-live-*` homes when done.
