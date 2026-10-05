<script lang="ts">
  import Activity from "@lucide/svelte/icons/activity";
  import ArrowLeft from "@lucide/svelte/icons/arrow-left";
  import Bot from "@lucide/svelte/icons/bot";
  import Info from "@lucide/svelte/icons/info";
  import Keyboard from "@lucide/svelte/icons/keyboard";
  import KeyRound from "@lucide/svelte/icons/key-round";
  import ListTree from "@lucide/svelte/icons/list-tree";
  import Settings2 from "@lucide/svelte/icons/settings-2";
  import ShieldCheck from "@lucide/svelte/icons/shield-check";
  import SquareTerminal from "@lucide/svelte/icons/square-terminal";
  import { t } from "../i18n/index.js";
  import { boot } from "../lib/boot.js";
  import { plainClick } from "../lib/nav.js";
  import { leaveSettings, navigate, SETTINGS_GROUPS, settingsHref, type SettingsSection } from "../lib/router.svelte.js";
  import { cn } from "../lib/utils.js";
  import AboutSection from "./settings/AboutSection.svelte";
  import CliSection from "./settings/CliSection.svelte";
  import EventsSection from "./settings/EventsSection.svelte";
  import GeneralSection from "./settings/GeneralSection.svelte";
  import ProvidersSection from "./settings/ProvidersSection.svelte";
  import RolesSection from "./settings/RolesSection.svelte";
  import RulesSection from "./settings/RulesSection.svelte";
  import ShortcutsSection from "./settings/ShortcutsSection.svelte";
  import StatusSection from "./settings/StatusSection.svelte";

  let { section, onsignout }: { section: SettingsSection; onsignout: () => void } = $props();

  const desktop = boot().desktop;
  const SECTIONS = {
    shortcuts: ShortcutsSection,
    about: AboutSection,
    roles: RolesSection,
    providers: ProvidersSection,
    cli: CliSection,
    rules: RulesSection,
    status: StatusSection,
    events: EventsSection,
  } as const;
  const ICONS = {
    general: Settings2,
    shortcuts: Keyboard,
    about: Info,
    roles: Bot,
    providers: KeyRound,
    cli: SquareTerminal,
    rules: ShieldCheck,
    status: Activity,
    events: ListTree,
  } as const satisfies Record<SettingsSection, unknown>;

  function go(event: MouseEvent, target: SettingsSection): void {
    if (!plainClick(event)) return;
    event.preventDefault();
    navigate(settingsHref(target), { replace: true });
  }
</script>

{#snippet link(target: SettingsSection, compact: boolean)}
  <a
    href={settingsHref(target)}
    class={cn(
      "group rounded-md text-sm text-foreground/85 transition-colors duration-100 hover:bg-accent focus-visible:outline-2 focus-visible:outline-primary/70",
      compact ? "shrink-0 px-2.5 py-1 whitespace-nowrap" : "flex items-center gap-2.5 px-2 py-1",
      target === section && "bg-accent text-foreground",
    )}
    aria-current={target === section ? "page" : undefined}
    onclick={(event) => go(event, target)}
    {@attach (node) => {
      // The phone strip scrolls sideways: the open section must not sit off screen.
      if (compact && target === section) node.scrollIntoView?.({ block: "nearest", inline: "center" });
    }}
  >
    {#if !compact}
      {@const Icon = ICONS[target]}
      <Icon size={16} class={cn("shrink-0 text-muted group-hover:text-foreground/85", target === section && "text-foreground")} aria-hidden="true" />
    {/if}
    {$t(`settings.sections.${target}`)}
  </a>
{/snippet}

<!-- A surface of its own, replacing the main window's layout until it is left. -->
<div class="flex h-full flex-col sm:flex-row">
  <nav class="flex shrink-0 flex-col border-b border-border bg-surface sm:w-60 sm:border-r sm:border-b-0" aria-label={$t("settings.nav")}>
    <!-- Desktop: the traffic lights sit in this row's left 78px. -->
    <div class={cn("drag-region flex h-[52px] shrink-0 items-center px-2", desktop && "sm:pl-[80px]")}>
      <button
        class="flex h-7 items-center gap-1.5 rounded-md px-2 text-sm text-muted hover:bg-accent hover:text-foreground focus-visible:outline-2 focus-visible:outline-primary/70"
        onclick={leaveSettings}
      >
        <ArrowLeft size={14} aria-hidden="true" />{$t("settings.back")}
      </button>
    </div>
    <div class="hidden min-h-0 flex-1 space-y-4 overflow-y-auto px-2 pt-2 pb-4 sm:block">
      {#each SETTINGS_GROUPS as group (group.id)}
        <div class="space-y-0.5" role="group" aria-labelledby={`settings-group-${group.id}`}>
          <h2 id={`settings-group-${group.id}`} class="px-2 pb-1 text-2xs font-medium text-muted">{$t(`settings.groups.${group.id}`)}</h2>
          {#each group.sections as target (target)}{@render link(target, false)}{/each}
        </div>
      {/each}
    </div>
    <!-- Phone width: the sections scroll sideways under the back button. -->
    <div class="flex gap-1 overflow-x-auto px-2 pb-2 [mask-image:linear-gradient(to_right,transparent,black_12px,black_calc(100%-32px),transparent)] sm:hidden">
      {#each SETTINGS_GROUPS.flatMap((group) => group.sections) as target (target)}{@render link(target, true)}{/each}
    </div>
  </nav>
  <section class="flex min-h-0 min-w-0 flex-1 flex-col" aria-labelledby="settings-section-title">
    <header class="drag-region flex h-[52px] shrink-0 items-center border-b border-border px-4 max-sm:hidden">
      <h1 id="settings-section-title" class="truncate text-base font-semibold tracking-tight">{$t(`settings.sections.${section}`)}</h1>
    </header>
    <div class="relative min-h-0 flex-1 overflow-y-auto overscroll-contain">
      <div class="mx-auto w-full max-w-[720px] space-y-6 px-4 py-6 sm:px-6">
        {#if section === "general"}
          <GeneralSection {onsignout} />
        {:else}
          {@const Section = SECTIONS[section]}
          {#key section}<Section />{/key}
        {/if}
      </div>
    </div>
  </section>
</div>
