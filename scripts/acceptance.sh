#!/usr/bin/env bash
# Agent-driven acceptance: a tester agent (the local Claude Code CLI) reads
# acceptance/scenarios.md, exercises the REAL system end to end and writes an
# evidence-based verdict report to .acceptance-report.json.
#
# Scenarios are natural language — cheap to change as requirements evolve, and
# able to express semantic checks ("the reply matches what actually happened")
# that assertion scripts cannot. This spends real model tokens, so it runs at
# the scope a change needs (see AGENTS.md → Verification):
#
#   scripts/acceptance.sh                    every scenario (before a release)
#   scripts/acceptance.sh --only 4F,5F,6E    just these
#   scripts/acceptance.sh --changed origin/main
#                                            the scenarios the changed paths touch
#   --model <id>                             the tester's model (default: the CLI's)
#   --behind-ok                              run even though the trunk has moved on
#
# It refuses to start while main or origin/main has commits this branch lacks:
# a run against code about to be rebased is spent again after the rebase.
# A model-gateway error (5xx, overload, rate limit) does not void a run: the
# tester's session is resumed where it stopped, up to three times.
set -euo pipefail

repo="$(cd "$(dirname "$0")/.." && pwd)"
report="$repo/.acceptance-report.json"
only="" changed="" model="" behind_ok=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --only) only="$2"; shift 2 ;;
    --changed) changed="$2"; shift 2 ;;
    --model) model="$2"; shift 2 ;;
    --behind-ok) behind_ok=1; shift ;;
    *) echo "usage: scripts/acceptance.sh [--only IDS | --changed REF] [--model ID] [--behind-ok]" >&2; exit 2 ;;
  esac
done

if [[ -z "$behind_ok" ]]; then
  for trunk in main origin/main; do
    git -C "$repo" rev-parse --verify -q "$trunk^{commit}" >/dev/null || continue
    behind="$(git -C "$repo" rev-list --count "HEAD..$trunk")"
    if [[ "$behind" -gt 0 ]]; then
      echo "error: $trunk has $behind commit(s) this branch lacks — rebase onto it first, or pass --behind-ok" >&2
      exit 1
    fi
  done
fi

# Which scenarios a path can break. Keep in step with acceptance/scenarios.md.
scenarios_for() {
  case "$1" in
    internal/guard/*) echo "5E 6B 7F" ;;
    internal/repos/*) echo "7A 7B 7C 7D 7E 7F 7G" ;;
    internal/agentcli/*) echo "1 5B 6A 6B 6E" ;;
    internal/agents/*) echo "1 1B 2 4B 4F 4I 4R 5A 5B 5C 5D 5F 5H 6E 7A 7B 7C 7E 7F 7G" ;;
    internal/kernel/*) echo "3 4 5G 5H" ;;
    internal/projections/*) echo "3 4O 4P 4R" ;;
    internal/api/*) echo "4E 4J 4K 4L 4N 4O 4P 4Q 7D 7E" ;;
    internal/connectors/*) echo "2 4H 4L" ;;
    internal/feishu/*) echo "4C 4D 4M" ;;
    internal/settings/*) echo "6A 6E" ;;
    internal/desktop/* | gui_on.go | gui_off.go | build/*) echo "6C" ;;
    internal/app/* | main.go) echo "1 6A 6C" ;;
    frontend/src/components/RunPicker.svelte | frontend/src/components/RunAsBar.svelte | frontend/src/components/Composer.svelte | frontend/src/lib/runAs.ts | frontend/src/lib/favorites.svelte.ts) echo "5I 6E" ;;
    frontend/src/components/WorktreesView.svelte | frontend/src/pages/ItemsPage.svelte | frontend/src/lib/worktrees.ts) echo "5I 7D" ;;
    frontend/*) echo "5I" ;;
  esac
}

if [[ -n "$changed" ]]; then
  paths="$(cd "$repo" && { git diff --name-only "$changed"...HEAD; git diff --name-only HEAD; git ls-files --others --exclude-standard; } | sort -u)"
  only="$(for p in $paths; do scenarios_for "$p"; done | tr ' ' '\n' | sed '/^$/d' | sort -u | paste -sd, -)"
  if grep -q '^acceptance/scenarios.md$' <<<"$paths"; then
    echo "note: acceptance/scenarios.md changed — add the scenarios you edited with --only" >&2
  fi
  if [[ -z "$only" ]]; then
    echo "nothing in the changes since $changed maps to a scenario; the automated tests cover it"
    exit 0
  fi
  echo "== scenarios touched since $changed: $only"
fi

rm -f "$report"
make -C "$repo" build-nogui fakeagent >/dev/null

scope="Execute EVERY scenario in acceptance/scenarios.md against the real system."
if [[ -n "$only" ]]; then
  scope="Execute ONLY these scenarios from acceptance/scenarios.md against the real system: ${only}. Read the rest of the file for context only; report just these."
fi

read -r -d '' charter <<EOF || true
You are the acceptance tester for hidane, a persistent personal agent runtime.
${scope}

Rules:
- Evidence or it didn't happen: every verdict must cite actual command output,
  file content, or API responses you observed in THIS run. Never mark PASS from
  assumptions or from reading source code.
- Prefer end-user surfaces (CLI, HTTP) to drive; corroborate through the event
  log (\`hidane events\`, /api/events) and the filesystem.
- Semantic checks matter: judge whether replies and artifacts genuinely match
  what happened, not just that commands exited 0.
- Judge each scenario against its written expectations only. FAIL means a
  written expectation is violated. What you notice beyond them — polish,
  wording, risks, ideas — goes to "notes", never to a FAIL: a run must be able
  to come back clean.
- Failures outside hidane — a model gateway's 5xx, rate limits, a CLI that is
  not logged in — are not FAILs: wait and retry; if they persist, mark the
  scenario BLOCKED with the exact reason.
- Spend the strong model where judgement is needed. Mechanical checks (status
  codes, event rows, files on disk) can go to sub-agents on a cheaper model
  such as sonnet; judging what a real model did stays with you.
- If the environment blocks a scenario, mark it BLOCKED with the exact reason.
- Use a fresh HIDANE_HOME under /tmp for every server you start, and stop every
  background process you started.
- When finished, write the report to .acceptance-report.json in the repo root:
  {"scenarios":[{"id":"1","name":"...","verdict":"PASS|FAIL|BLOCKED","evidence":"<concrete observed evidence, concise>"}],
   "summary":{"pass":N,"fail":N,"blocked":N},"notes":"<anything worth flagging>"}
  Then print exactly: ACCEPTANCE DONE
EOF

session="$(python3 -c 'import uuid; print(uuid.uuid4())')"
transcript="$(mktemp -t hidane-acceptance)"
tester() {
  # Print mode otherwise ends the tester 10 minutes after its last turn while
  # its background sub-agents (the browser UI check) are still working, and no
  # report gets written.
  CLAUDE_CODE_PRINT_BG_WAIT_CEILING_MS=0 env -u CLAUDECODE claude -p --permission-mode bypassPermissions \
    ${model:+--model "$model"} --append-system-prompt "$charter" "$@" 2>&1 | tee -a "$transcript"
}

echo "== agent-driven acceptance: spawning the tester agent (session $session) =="
cd "$repo"
tester --session-id "$session" \
  "Run the acceptance now. The hidane binaries are at bin/hidane-nogui (headless) and bin/fake/{claude,codex,pi} (protocol-faithful fakes)." || true

for attempt in 1 2 3; do
  [[ -f "$report" ]] && break
  if ! tail -n 40 "$transcript" | grep -qE 'API Error: (5[0-9][0-9]|429)|overloaded|rate.?limit|ECONNRESET|socket hang up|fetch failed'; then
    break
  fi
  echo "== the model gateway failed; resuming the tester in 60s (attempt $attempt of 3) =="
  sleep 60
  tester --resume "$session" \
    "You were interrupted by a model API error. Continue where you stopped: keep the verdicts you already reached, finish the remaining scenarios, then write the report." || true
done

if [[ ! -f "$report" ]]; then
  echo "no report written (tester transcript: $transcript)" >&2
  exit 1
fi
python3 - "$report" <<'PY'
import json, sys
r = json.load(open(sys.argv[1]))
for s in r["scenarios"]:
    print(f'{s["verdict"]:8} {s["id"]:>4}  {s["name"]}')
print(r["summary"])
sys.exit(1 if r["summary"].get("fail", 0) else 0)
PY
