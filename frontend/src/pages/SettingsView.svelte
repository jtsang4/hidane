<script lang="ts">
  import { ArrowLeft } from "@lucide/svelte";
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
      "rounded-md text-sm text-foreground/85 hover:bg-surface-2 focus-visible:outline-2 focus-visible:outline-primary",
      compact ? "shrink-0 px-3 py-1.5 whitespace-nowrap" : "block px-2.5 py-1.5",
      target === section && "bg-surface-2 text-foreground",
    )}
    aria-current={target === section ? "page" : undefined}
    onclick={(event) => go(event, target)}
  >{$t(`settings.sections.${target}`)}</a>
{/snippet}

<!-- A surface of its own, replacing the main window's layout until it is left. -->
<div class="flex h-full flex-col sm:flex-row">
  <nav class="flex shrink-0 flex-col border-b border-border bg-surface sm:w-60 sm:border-r sm:border-b-0" aria-label={$t("settings.nav")}>
    <!-- Desktop: the traffic lights sit in this row's left 78px. -->
    <div class={cn("drag-region flex h-[52px] shrink-0 items-center px-2", desktop && "sm:pl-[80px]")}>
      <button
        class="flex h-8 items-center gap-1.5 rounded-md px-2 text-sm text-muted hover:bg-surface-2 hover:text-foreground focus-visible:outline-2 focus-visible:outline-primary"
        onclick={leaveSettings}
      >
        <ArrowLeft size={16} aria-hidden="true" />{$t("settings.back")}
      </button>
    </div>
    <div class="hidden min-h-0 flex-1 space-y-4 overflow-y-auto px-2 pt-2 pb-4 sm:block">
      {#each SETTINGS_GROUPS as group (group.id)}
        <div class="space-y-0.5" role="group" aria-labelledby={`settings-group-${group.id}`}>
          <h2 id={`settings-group-${group.id}`} class="px-2.5 pb-1 text-[11px] font-medium text-muted">{$t(`settings.groups.${group.id}`)}</h2>
          {#each group.sections as target (target)}{@render link(target, false)}{/each}
        </div>
      {/each}
    </div>
    <!-- Phone width: the sections scroll sideways under the back button. -->
    <div class="flex gap-1 overflow-x-auto px-2 pb-2 sm:hidden">
      {#each SETTINGS_GROUPS.flatMap((group) => group.sections) as target (target)}{@render link(target, true)}{/each}
    </div>
  </nav>
  <section class="flex min-h-0 min-w-0 flex-1 flex-col" aria-labelledby="settings-section-title">
    <header class="drag-region flex h-[52px] shrink-0 items-center border-b border-border px-6">
      <h1 id="settings-section-title" class="truncate text-sm font-semibold">{$t(`settings.sections.${section}`)}</h1>
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
