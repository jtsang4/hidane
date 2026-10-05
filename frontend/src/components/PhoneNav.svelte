<script lang="ts">
  import Settings from "@lucide/svelte/icons/settings";
  import { t } from "../i18n/index.js";
  import { MAIN_NAV, plainClick } from "../lib/nav.js";
  import { navigate, openSettings, routeFor, routerState } from "../lib/router.svelte.js";
  import { cn } from "../lib/utils.js";

  let route = $derived(routeFor(routerState.path));

  function active(path: string): boolean {
    return path === "/" ? route.name === "chat" : routerState.path === path;
  }

  const item = "flex min-w-0 flex-1 items-center justify-center rounded-md px-2 py-2 text-muted hover:bg-accent focus-visible:outline-2 focus-visible:outline-primary/70";
</script>

<!-- Phone-width browsers only: the desktop window's minimum width never reaches this. -->
<nav class="flex shrink-0 justify-around border-t border-border bg-surface p-2 pb-[max(0.5rem,env(safe-area-inset-bottom))] sm:hidden" aria-label={$t("shell.mainNav")}>
  {#each MAIN_NAV as entry (entry.to)}
    {@const Icon = entry.icon}
    <a
      href={entry.to}
      class={cn(item, active(entry.to) && "bg-accent text-foreground")}
      aria-label={$t(entry.key)}
      aria-current={active(entry.to) ? "page" : undefined}
      onclick={(event) => {
        if (!plainClick(event)) return;
        event.preventDefault();
        navigate(entry.to);
      }}
    >
      <Icon size={18} aria-hidden="true" />
    </a>
  {/each}
  <button class={item} aria-label={$t("shell.settings")} onclick={() => openSettings()}>
    <Settings size={18} aria-hidden="true" />
  </button>
</nav>
