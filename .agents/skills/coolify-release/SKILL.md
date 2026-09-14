---
name: coolify-release
description: Publish the project by synchronizing tested changes to the remote release branch. Use when a change is ready to be deployed or the user asks how to release it.
---

# Release through the `release` branch

This project is deployed from the remote `release` branch. Once a change is
committed and pushed to that branch, the configured deployment system rebuilds
and deploys it automatically. No separate manual publish step is required.

## Workflow

1. Review the worktree and confirm the intended changes are committed.
2. Run the relevant checks before publishing. For a normal code change, use
   `pnpm typecheck` and `pnpm test`; for frontend changes also run the web
   checks described in the repository instructions.
3. Push the commit to the remote `release` branch:

   ```bash
   git push origin HEAD:release
   ```

   If the current branch is already `release`, `git push origin release` is
   equivalent.

4. Confirm that the remote branch contains the commit, then monitor the
   deployment in the configured deployment console until the build and health
   checks succeed.

Do not push unreviewed work, force-push the release branch, or put credentials
in commits. If the remote branch has advanced, fetch it and resolve the
divergence before pushing:

```bash
git fetch origin release
git log --oneline --decorate --max-count=5 origin/release
```
