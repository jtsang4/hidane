<script lang="ts">
  import GitBranch from "@lucide/svelte/icons/git-branch";
  import { t } from "../i18n/index.js";
  import type { CardCheckout } from "../lib/api.js";
  import { cn } from "../lib/utils.js";

  /** Where a task's work lives: the repository, the branch it is on, and what is wrong with it. */
  let { checkout: c, class: className = "" }: { checkout: CardCheckout; class?: string } = $props();
</script>

<p class={cn("flex min-w-0 items-center gap-1 text-xs", c.missing ? "text-danger" : "text-muted", className)} title={$t("worktrees.branchOf", { repo: c.repo, branch: c.branch })}>
  <GitBranch size={12} class="shrink-0" aria-hidden="true" /><span class="shrink-0">{c.repo}</span><span class="truncate font-mono">{c.branch}</span>
  {#if c.continues}<span class="truncate">· {$t("worktrees.continues", { branch: c.continues })}</span>{/if}
  {#if c.mode === "in_place"}<span class="shrink-0">· {$t("worktrees.inPlace")}</span>{/if}
  {#if c.missing}<span class="shrink-0">· {$t("worktrees.status.repoMissing")}</span>{:else if c.setup === "running"}<span class="shrink-0">· {$t("worktrees.status.setup")}</span>{:else if c.setup === "failed"}<span class="shrink-0 text-danger">· {$t("worktrees.status.setupFailed")}</span>{/if}
</p>
