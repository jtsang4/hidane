#!/usr/bin/env bash
# Agent-driven acceptance: a tester agent (the local Claude Code CLI) reads
# acceptance/scenarios.md, exercises the REAL system end to end and writes an
# evidence-based verdict report to .acceptance-report.json.
#
# Scenarios are natural language — cheap to change as requirements evolve, and
# able to express semantic checks ("the reply matches what actually happened")
# that assertion scripts cannot. This spends real model tokens.
set -euo pipefail

repo="$(cd "$(dirname "$0")/.." && pwd)"
report="$repo/.acceptance-report.json"
rm -f "$report"

make -C "$repo" build-nogui fakeagent >/dev/null

read -r -d '' charter <<'EOF' || true
You are the acceptance tester for hidane, a persistent personal agent runtime.
Execute EVERY scenario in acceptance/scenarios.md against the real system.

Rules:
- Evidence or it didn't happen: every verdict must cite actual command output,
  file content, or API responses you observed in THIS run. Never mark PASS from
  assumptions or from reading source code.
- Prefer end-user surfaces (CLI, HTTP) to drive; corroborate through the event
  log (`hidane events`, /api/events) and the filesystem.
- Semantic checks matter: judge whether replies and artifacts genuinely match
  what happened, not just that commands exited 0.
- If the environment blocks a scenario, mark it BLOCKED with the exact reason.
- Use a fresh HIDANE_HOME under /tmp for every server you start, and stop every
  background process you started.
- When finished, write the report to .acceptance-report.json in the repo root:
  {"scenarios":[{"id":"1","name":"...","verdict":"PASS|FAIL|BLOCKED","evidence":"<concrete observed evidence, concise>"}],
   "summary":{"pass":N,"fail":N,"blocked":N},"notes":"<anything worth flagging>"}
  Then print exactly: ACCEPTANCE DONE
EOF

echo "== agent-driven acceptance: spawning the tester agent =="
cd "$repo"
# Print mode otherwise ends the tester 10 minutes after its last turn while
# its background sub-agents (the browser UI check) are still working, and no
# report gets written.
CLAUDE_CODE_PRINT_BG_WAIT_CEILING_MS=0 env -u CLAUDECODE claude -p --permission-mode bypassPermissions --append-system-prompt "$charter" \
  "Run the acceptance now. The hidane binaries are at bin/hidane-nogui (headless) and bin/fake/{claude,codex,pi} (protocol-faithful fakes)."

if [[ ! -f "$report" ]]; then
  echo "no report written" >&2
  exit 1
fi
python3 - "$report" <<'EOF'
import json, sys
r = json.load(open(sys.argv[1]))
for s in r["scenarios"]:
    print(f'{s["verdict"]:8} {s["id"]:>4}  {s["name"]}')
print(r["summary"])
sys.exit(1 if r["summary"].get("fail", 0) else 0)
EOF
