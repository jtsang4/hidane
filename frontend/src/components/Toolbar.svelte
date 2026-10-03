<script lang="ts">
  import type { Snippet } from "svelte";
  import { PanelLeft, Search } from "@lucide/svelte";
  import { t } from "../i18n/index.js";
  import { boot } from "../lib/boot.js";
  import { formatShortcut, isMacPlatform } from "../lib/commands.js";
  import { setSidebarCollapsed, ui } from "../lib/ui.svelte.js";
  import { cn } from "../lib/utils.js";

  let { title, actions }: { title: string; actions?: Snippet } = $props();

  const desktop = boot().desktop;
  const mac = isMacPlatform();
</script>

<!-- The window has no title bar of its own: this row is where it is dragged from. -->
<header
  class={cn(
    "drag-region flex h-[52px] shrink-0 items-center gap-2 border-b border-border px-4",
    ui.sidebarCollapsed && desktop && "sm:pl-[84px]",
  )}
>
  {#if ui.sidebarCollapsed}
    <button
      class="hidden size-7 shrink-0 items-center justify-center rounded-md text-muted hover:bg-accent hover:text-foreground focus-visible:outline-2 focus-visible:outline-primary/70 sm:flex"
      aria-label={$t("shell.expandSidebar")}
      title={`${$t("shell.expandSidebar")} (${formatShortcut("b", mac)})`}
      onclick={() => setSidebarCollapsed(false)}
    >
      <PanelLeft size={16} aria-hidden="true" />
    </button>
  {/if}
  <h1 class="min-w-0 truncate text-base font-semibold tracking-tight">{title}</h1>
  <div class="ml-auto flex min-w-0 shrink-0 items-center gap-1.5">
    {@render actions?.()}
    <button
      class="flex size-7 items-center justify-center rounded-md text-muted coarse:size-9 hover:bg-accent hover:text-foreground focus-visible:outline-2 focus-visible:outline-primary/70 sm:hidden"
      aria-label={$t("shell.search")}
      onclick={() => (ui.paletteOpen = true)}
    >
      <Search size={16} aria-hidden="true" />
    </button>
  </div>
</header>
