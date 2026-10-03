#!/usr/bin/env bash
# One real hidane task on the locally installed agent CLIs, in a fresh home.
# See ../SKILL.md. Spends real model tokens.
set -euo pipefail

worker="" all="" model="haiku" policy="" steer="" steer_after=3
while [[ $# -gt 1 ]]; do
  case "$1" in
    --worker) worker="$2"; shift 2 ;;
    --all) all="$2"; shift 2 ;;
    --model) model="$2"; shift 2 ;;
    --policy) policy="$2"; shift 2 ;;
    --steer) steer="$2"; shift 2 ;;
    --steer-after) steer_after="$2"; shift 2 ;;
    *) break ;;
  esac
done
task="${1:?usage: live-check.sh [--worker cli | --all cli] [--model id] [--policy regex] [--steer text [--steer-after s]] \"task\"}"

repo="$(git -C "$(dirname "$0")" rev-parse --show-toplevel)"
bin="$repo/bin/hidane-nogui"
(cd "$repo" && CGO_ENABLED=0 go build -tags nogui -o "$bin" .)

home="$(mktemp -d /tmp/hidane-live-XXXX)"
python3 - "$home" "$worker" "$all" "$model" "$policy" <<'PY'
import json, sys
home, worker, every, model, policy = sys.argv[1:]
def role(agent):
    return {"agent": agent, "provider": "", "model": model if agent == "claude" else "", "effort": ""}
names = ["primary", "manager", "worker", "distiller"]
roles = {r: role(every or "claude") for r in names}
if worker:
    roles["worker"] = role(worker)
with open(f"{home}/settings.json", "w") as f:
    json.dump({"providers": [], "roles": roles, "binaries": {"claude": "", "codex": "", "pi": ""}}, f)
if policy:
    with open(f"{home}/POLICY.json", "w") as f:
        json.dump({"rules": [{"id": "pol_live", "pattern": policy, "reason": "refused by the live check"}]}, f)
PY

echo "== home: $home"
if [[ -n "$steer" ]]; then
  # The person's words, sent to the task once its worker has made its first
  # tool call (plus --steer-after seconds), so they land mid-run.
  (
    wi=""
    for _ in $(seq 1 540); do
      wi="$(HIDANE_HOME="$home" "$bin" events --tail 200 2>/dev/null | grep -m1 "side_effect.intent" | grep -o '\[wi_[^]]*\]' | tr -d '[]' || true)"
      [[ -n "$wi" ]] && break
      sleep 1
    done
    sleep "$steer_after"
    echo
    echo "== steering $wi: $steer"
    HIDANE_HOME="$home" "$bin" chat --item "$wi" --timeout 5 "$steer" >/dev/null || true
  ) &
fi
HIDANE_HOME="$home" "$bin" chat --timeout 540 "$task" || true
wait
echo
echo "== decisions, executions, refusals, replies"
HIDANE_HOME="$home" "$bin" events --tail 200 \
  | grep -E "route.decision|work_item.created|manager.decision|execution.steered|execution.finished|policy.blocked|agent.reply|agent.error" || true
echo
echo "== files produced (outside .hidane)"
find "$home" -type f -not -path "*/.hidane/*" -not -path "$home/runtime/*" -not -path "$home/sessions/*" \
  -not -name 'hidane.db*' -not -name settings.json -not -name POLICY.json -not -name runtime.lock | sed "s|$home/||"
