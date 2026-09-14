---
name: coolify-release
description: Publish the project by merging tested changes into remote main before synchronizing the release branch. Use when a change is ready to be deployed or the user asks how to release it.
---

# Release through the `release` branch

The trunk branch is `main`; production deploys from the remote `release` branch.
Pushing to `release` automatically rebuilds and deploys the project, so that push
is the publication step.

Every release commit must already be reachable from **remote `main`** before it
is pushed to `release`. A commit on local `main` is not enough. Never publish
directly from a feature branch or let `release` contain commits absent from
remote `main`.

## Workflow

1. Review the worktree and identify the intended changes. Preserve unrelated
   work; use an isolated worktree when needed. Fetch both remote branches:

   ```bash
   git fetch origin main release
   git log --oneline origin/main..origin/release
   ```

2. If `release` is ahead of or diverged from `main`, first merge `origin/release`
   into `main`, preserving its commit ancestry. Do not replace that merge with
   a squash or cherry-pick: the release commits themselves must be contained in
   `main`. Resolve conflicts and include the intended feature changes in `main`
   through the repository's normal merge or PR workflow.

3. Run `pnpm typecheck` and `pnpm test` before committing, plus the additional
   web checks required by the repository instructions for frontend changes.
   Commit the intended changes and push or merge them to **remote `main` first**.
   Validate the final integrated revision if merging introduced changes that
   were not covered by the checks. If branch protection requires a PR, finish
   merging that PR before proceeding; do not bypass it by pushing to `release`.

4. Fetch again and select the tested revision from remote `main`:

   ```bash
   git fetch origin main release
   release_commit=$(git rev-parse origin/main)
   git merge-base --is-ancestor origin/release "$release_commit"
   ```

   The ancestry check must succeed: `release` may be equal to or behind `main`,
   never ahead of or diverged from it. If it fails, return to the merge step.
   Confirm that `release_commit` is the exact integrated revision that passed
   validation; if the remote branches advanced, review and validate the newly
   included changes before continuing.

5. Immediately before publishing, refresh the remote refs and require both
   ancestry checks to pass. Publish only the selected commit already in remote
   `main`, using a normal fast-forward push:

   ```bash
   git fetch origin main release
   git merge-base --is-ancestor "$release_commit" origin/main &&
     git merge-base --is-ancestor origin/release "$release_commit" &&
     git push origin "$release_commit:refs/heads/release"
   ```

   If a check or push fails, fetch and reconcile the branches before retrying.
   Never force-push either branch or fall back to publishing arbitrary `HEAD`.

6. Confirm the remote `release` tip equals the selected commit and is an ancestor
   of remote `main`. Monitor the deployment console until the build and health
   checks succeed. If the selected commit is already deployed and no runtime
   configuration changed, no additional deployment is needed.

Keep credentials out of commits. Do not publish unreviewed work.
