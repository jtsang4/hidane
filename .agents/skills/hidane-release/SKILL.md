---
name: hidane-release
description: Publish a hidane version — choose the semver number, run the release gates, dry-run the packaging locally, then push a `v*` tag so .github/workflows/release.yml builds the macOS, Windows and Linux desktop apps and creates the GitHub release; watch it and verify the published assets. Use whenever the user asks to release, ship, tag or publish a version (including prereleases such as v1.2.0-rc.1), or to check on or repair a release.
---

# Releasing hidane

A release is a **pushed version tag**. `.github/workflows/release.yml` runs on
tags matching `v1.2.3` or `v1.2.3-<prerelease>` and on nothing else:

| Job | Runs on | Produces |
|---|---|---|
| `verify` | ubuntu | the test gates again (frontend check/test/build, `go vet` + `go test -race` nogui, Windows `go vet`) |
| `macos` | macos-latest | `hidane-<v>-macos-universal.dmg` + `.zip` (arm64 + x86_64; Developer ID–signed and notarized when the secrets exist, ad-hoc signed otherwise) — the packaged app must open its window |
| `windows` | ubuntu (cross-compiled, no cgo) | `hidane-<v>-windows-{amd64,arm64}.zip` with `hidane.exe` (GUI, icon + manifest embedded) and `hidane-cli.exe` (console) |
| `windows-smoke` | windows-latest | the amd64 `hidane.exe` must open its window |
| `linux` | ubuntu-24.04 / ubuntu-24.04-arm | `hidane-<v>-linux-{amd64,arm64}.tar.gz` + `.deb` (GTK 4 + WebKitGTK 6.0, Wails v3's default backend) — must open its window under Xvfb |
| `publish` | ubuntu | `SHA256SUMS`, then `gh release create` with `build/release-notes.md` + generated notes; a tag with `-` is a prerelease |

Packaging itself is `scripts/package.sh` — the same script locally and in CI.
A tag runs the workflow **as it exists in the tagged commit**, so the commit
must contain `.github/workflows/release.yml`.

## 1. Preconditions

```sh
git status --short                 # must be empty: never release a dirty tree
git fetch origin --tags
git branch --show-current          # normally main
git status -sb | head -1           # not ahead of / behind origin
gh auth status                     # gh can see the repository
```

Release from the default branch unless the user names another ref. If the
commits to release are not on `origin` yet, they must be pushed first — ask
the user before pushing a branch.

## 2. Choose the version

```sh
last="$(git describe --tags --abbrev=0 2>/dev/null || true)"; echo "last release: ${last:-none}"
git log --no-merges --format='%s' ${last:+"$last"..}HEAD
```

Commits are Conventional Commits. Semver from what they say: a breaking change
(`type!:` or `BREAKING CHANGE`) → major (before 1.0: minor); any `feat` → minor;
only `fix` / `perf` / `refactor` / `docs` / `chore` → patch. A prerelease is
`vX.Y.Z-rc.N` (or `-beta.N`, `-alpha.N`); counting continues across retries
(`rc.1`, `rc.2`, …). The first release ever is the user's call — suggest
`v0.1.0`.

If the user did not state the version, propose one with the reasoning (the
commit subjects that decided it) and get it confirmed. Check it is unused:
`git rev-parse -q --verify "refs/tags/$version"` must print nothing.

## 3. Gates

All must pass on the exact commit being tagged (see AGENTS.md → Verification):

```sh
make test
make e2e
make smoke-gui        # on macOS: the real Wails window
```

Acceptance by the size of the release (it spends real tokens):

- patch: `make acceptance ARGS="--changed <last tag>"` — only the scenarios
  the changes since the last release touch (it says so when none are);
- minor or major: the full `make acceptance` (about an hour) — offer it, run
  it if the user agrees.

Only FAIL blocks a release; a BLOCKED for missing credentials or permissions
does not, but say which.

## 4. Dry-run the packaging

```sh
.agents/skills/hidane-release/scripts/dry-run.sh "$version-dryrun"
```

It builds every artifact the workflow builds into `bin/release/` and opens the
macOS app and (in Docker) the Linux app headlessly; each must print
`ui ready (live transport: wails)`. Windows is cross-compiled only — its window
is checked by the workflow's `windows-smoke` job. A dry run never publishes.

## 5. Tag and push — only with the user's explicit go-ahead

Pushing a tag publishes a release to everyone; say exactly what will happen
(version, commit, prerelease or not) and wait for a yes.

```sh
git tag -a "$version" -m "hidane $version"
git push origin "$version"
```

Never re-point or force-push an existing tag.

## 6. Watch and verify

```sh
run="$(gh run list --workflow release.yml --event push --limit 5 --json databaseId,headBranch \
  --jq ".[] | select(.headBranch == \"$version\") | .databaseId" | head -1)"
gh run watch "$run" --exit-status     # ~15–25 minutes
gh release view "$version" --json url,isPrerelease,assets --jq '{url, isPrerelease, assets: [.assets[].name]}'
```

Expect 9 assets: the macOS `.dmg` and `.zip`, two Windows `.zip`, two Linux
`.tar.gz`, two `.deb`, and `SHA256SUMS`. Then check the published bytes, not
the build log:

```sh
dir="$(mktemp -d)" && cd "$dir"
gh release download "$version" --repo jtsang4/hidane
shasum -a 256 -c SHA256SUMS
ditto -x -k "hidane-$version-macos-universal.zip" app        # on macOS
HIDANE_HOME="$(mktemp -d)" HIDANE_GUI_SMOKE=1 HIDANE_LOGIN_SHELL=0 app/hidane.app/Contents/MacOS/hidane
app/hidane.app/Contents/MacOS/hidane version                 # prints the tag
```

Report the release URL and what you verified.

## When it goes wrong

- **A job failed, nothing was published** (no release exists): read the log
  (`gh run view "$run" --log-failed`), fix on the branch through the normal
  commit flow, then — with the user's consent — delete the tag and tag the
  fixed commit: `git push origin :refs/tags/$version && git tag -d $version`.
  Re-running the same commit (`gh run rerun "$run" --failed`) is fine for a
  flaky runner.
- **A release was published and is broken**: do not move the tag. Ship the fix
  as the next patch version (or the next `-rc.N`). Removing the broken release
  (`gh release delete`) is the user's decision.
- **macOS users see "cannot be opened"**: the build had no Developer ID secrets
  and is ad-hoc signed; the release notes tell users how to open it. Proper
  signing needs these repository secrets: `MACOS_CERTIFICATE` (base64 of the
  Developer ID Application `.p12`), `MACOS_CERTIFICATE_PASSWORD`,
  `MACOS_SIGN_IDENTITY` (e.g. `Developer ID Application: Name (TEAMID)`), and
  for notarization `APPLE_ID`, `APPLE_TEAM_ID`, `APPLE_APP_PASSWORD` (an
  app-specific password). Never put them anywhere but the repository secrets.
