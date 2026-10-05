---
name: hidane-local-build
description: Update hidane from its Git upstream, compile and verify a local macOS desktop app, then install and launch it. Use for requests to pull the latest code, rebuild a local version, or install hidane locally, including “拉取最新代码并编译” and “重新编译本地版本”. Publishing a release belongs to hidane-release.
---

# Build and install hidane locally

Deliver a working desktop app from the requested checkout, normally at
`~/Applications/Hidane.app`. Read the repository's `AGENTS.md` and `Makefile`
first; use their current build targets instead of recreating the build system.
This workflow updates a local installation. Commit or push repository changes
only when the user requests those actions.

## Update the checkout

Run from the repository root:

```sh
git status --short --branch
git remote -v
git rev-parse --abbrev-ref '@{u}'
```

When the user wants the latest code and the tree is clean, pull the current
branch's upstream with `git pull --ff-only`. This also checks for updates when
the user says they already pulled. Honor a specified branch or ref instead;
do not switch branches implicitly. If the upstream is missing, branches have
diverged, or local edits prevent the update, resolve that condition with the
user. Do not reset, discard, or automatically stash their work. Building local
edits is fine when requested; report that the build includes them.

Record `git rev-parse --short HEAD` and whether the tree is dirty after updating.
Do not push a version tag as part of a local build.

## Check the toolchain and build

The desktop procedure below requires macOS, Go at least 1.26.8, Node at least 24,
pnpm, Python 3 for the smoke check, and Apple's command-line tools.
Check `go version`, `node --version`,
`pnpm --version`, `uname -m`, and `xcrun --find clang`. The Makefile builds for
the host architecture and installs frontend dependencies from the frozen
pnpm lockfile; the Wails CLI is not needed for `make app`.

```sh
make app
make test
make fakeagent
plutil -lint bin/Hidane.app/Contents/Info.plist
codesign --force --sign - bin/Hidane.app
codesign --verify --strict bin/Hidane.app
```

Run expensive build and test commands sequentially. If the user is concerned
about heat, prefix them with `GOMAXPROCS=4` to limit Go parallelism for those
commands only. Leave global toolchain settings alone. Apply additional checks
from `AGENTS.md` when source changes require them; a local rebuild by itself
does not require a paid live-model acceptance run.

## Verify the real desktop window

Quit an existing hidane window gracefully before the smoke run: Wails' single
instance handling can otherwise activate the old app instead of loading the
new one. Use the available native app controls or ask the user to close it if
none are available. Do not kill an unrelated agent CLI or force-kill active
work to make the check pass.

Use a temporary home, absolute fake CLI paths, and `HIDANE_LOGIN_SHELL=0`.
The bundled app is tested below so the check also covers bundle startup.
`make smoke-gui` creates its own home without fake CLI configuration; use this
equivalent invocation to keep CLI discovery isolated from the user's accounts.

```sh
python3 - <<'PY'
import json
import os
from pathlib import Path
import subprocess
import tempfile

repo = Path.cwd()
binary = repo / "bin/Hidane.app/Contents/MacOS/hidane"
log = repo / "bin/gui-smoke.log"
with tempfile.TemporaryDirectory(prefix="hidane-gui-smoke-") as home:
    settings = Path(home) / "settings.json"
    settings.write_text(json.dumps({"binaries": {
        cli: str(repo / "bin/fake" / cli)
        for cli in ("claude", "codex", "pi")
    }}))
    settings.chmod(0o600)
    env = {key: value for key, value in os.environ.items()
           if not key.startswith(("HIDANE_", "FEISHU_"))}
    env.update(HIDANE_HOME=home, HIDANE_LOGIN_SHELL="0", HIDANE_GUI_SMOKE="1")
    with log.open("w") as output:
        result = subprocess.run([str(binary)], env=env, stdout=output,
                                stderr=subprocess.STDOUT, timeout=120)
    output = log.read_text()
    print(output)
    if result.returncode or "ui ready (live transport: wails)" not in output:
        raise SystemExit("Desktop startup verification failed")
PY
```

The smoke app exits after reaching the backend through the Wails live channel.
Check both its exit code and the readiness line. Keep failures visible and fix
them before replacing the user's installed app. Do not send a test chat to a
real model merely to verify that the window opens.

## Install and launch

Use the user's requested installation path, or `~/Applications/Hidane.app` by
default. If it, or a bundle from before the rename (`hidane.app`), exists,
move the old bundle to a fresh backup directory before
copying, so the replacement is a complete bundle and rollback remains possible.
Copy the verified build with `ditto`; verify the installed signature with
`codesign --verify --strict`. If copying or verification fails, restore the
previous bundle. Do not alter the user's `~/.hidane` data or model settings.

Launch the installed bundle with `open`, with stdout and stderr directed to
`bin/hidane-local.log`. Do not carry `HIDANE_GUI_SMOKE`, the temporary home, or
fake CLI settings into this normal launch. For the default path:

```sh
open -a "$HOME/Applications/Hidane.app" \
  --stdout "$PWD/bin/hidane-local.log" --stderr "$PWD/bin/hidane-local.log"
"$HOME/Applications/Hidane.app/Contents/MacOS/hidane" version
```

Confirm the installed process remains running and its startup log contains
`ui ready (live transport: wails)`. Inspect the real window when native UI
tools are available. Leave it open for the user. Report the installed path,
build version, verification result, and any material limitation. Keep binaries,
logs, temporary settings, and backups out of commits.
