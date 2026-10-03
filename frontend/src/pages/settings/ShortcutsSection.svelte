<script lang="ts">
  import { t } from "../../i18n/index.js";
  import { formatShortcut, isMacPlatform, SHORTCUTS, type Command } from "../../lib/commands.js";
  import SettingsCard from "../../components/settings/SettingsCard.svelte";
  import SettingsRow from "../../components/settings/SettingsRow.svelte";

  const mac = isMacPlatform();

  const LABELS = {
    "open-settings": "settings.shortcuts.openSettings",
    "new-task": "settings.shortcuts.newTask",
    "focus-composer": "settings.shortcuts.focusComposer",
    "go:chat": "nav.chat",
    "go:items": "nav.items",
    "go:schedules": "nav.schedules",
    "go:memory": "nav.memory",
    "go:log": "nav.log",
    search: "settings.shortcuts.search",
    "toggle-sidebar": "settings.shortcuts.toggleSidebar",
  } as const satisfies Record<Command, string>;

  const GO: readonly Command[] = ["go:chat", "go:items", "go:schedules", "go:memory", "go:log"];
  const app = SHORTCUTS.filter((shortcut) => !GO.includes(shortcut.command));
  const go = SHORTCUTS.filter((shortcut) => GO.includes(shortcut.command));
</script>

{#snippet keys(text: string)}
  <kbd class="rounded border border-border bg-accent px-1.5 py-px font-sans text-2xs text-foreground">{text}</kbd>
{/snippet}

<p class="text-xs text-muted">{$t("settings.shortcuts.hint")}</p>

<SettingsCard title={$t("settings.shortcuts.app")}>
  {#each app as shortcut (shortcut.command)}
    <SettingsRow label={$t(LABELS[shortcut.command])}>{@render keys(formatShortcut(shortcut.key, mac))}</SettingsRow>
  {/each}
  <SettingsRow label={$t("settings.shortcuts.escape")}>{@render keys("Esc")}</SettingsRow>
</SettingsCard>

<SettingsCard title={$t("settings.shortcuts.go")}>
  {#each go as shortcut (shortcut.command)}
    <SettingsRow label={$t(LABELS[shortcut.command])}>{@render keys(formatShortcut(shortcut.key, mac))}</SettingsRow>
  {/each}
</SettingsCard>

<SettingsCard title={$t("settings.shortcuts.composer")}>
  <SettingsRow label={$t("settings.shortcuts.send")}>{@render keys("Enter")}</SettingsRow>
  <SettingsRow label={$t("settings.shortcuts.newline")}>{@render keys(mac ? "⇧Enter" : "Shift+Enter")}</SettingsRow>
</SettingsCard>
