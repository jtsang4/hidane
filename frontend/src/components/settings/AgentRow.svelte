<script lang="ts">
  import { t } from "../../i18n/index.js";
  import type { AgentInfo, AgentKind } from "../../lib/api.js";
  import { errorText } from "../../lib/settings.js";
  import Badge from "../ui/Badge.svelte";
  import Input from "../ui/Input.svelte";
  import SaveStatus from "./SaveStatus.svelte";
  import SettingsRow from "./SettingsRow.svelte";

  let {
    kind,
    info,
    detecting,
    savedPath,
    onsave,
  }: {
    kind: AgentKind;
    info: AgentInfo | undefined;
    detecting: boolean;
    /** The configured override; `""` means look the binary up on PATH. */
    savedPath: string;
    onsave: (path: string) => Promise<unknown>;
  } = $props();

  let path = $derived(savedPath);
  let saving = $state(false);
  let saveError = $state<string | null>(null);
  let savedOnce = $state(false);
  let dirty = $derived(path.trim() !== savedPath);
  let inputId = $derived(`agent-path-${kind}`);
  let status = $derived<"idle" | "saving" | "saved" | "error">(saving ? "saving" : saveError ? "error" : savedOnce && !dirty ? "saved" : "idle");

  /** Committed on blur or Enter: a half-typed path is not worth a detection run. */
  async function commit(): Promise<void> {
    if (!dirty || saving) return;
    saving = true;
    saveError = null;
    try {
      await onsave(path.trim());
      savedOnce = true;
    } catch (error) {
      saveError = errorText(error);
    } finally {
      saving = false;
    }
  }
</script>

<div class="space-y-2" role="group" aria-labelledby={`agent-row-${kind}`}>
  <div class="flex flex-wrap items-center gap-2 px-1">
    <h2 id={`agent-row-${kind}`} class="text-[13px] font-semibold">{$t(`settings.kinds.${kind}`)}</h2>
    {#if detecting && !info}
      <Badge tone="muted">{$t("settings.agents.detecting")}</Badge>
    {:else if info?.available}
      <Badge tone="success">{$t("settings.agents.available")}</Badge>
      {#if info.version}<span class="font-mono text-xs text-muted">{$t("settings.agents.version", { version: info.version })}</span>{/if}
    {:else}
      <Badge tone="danger">{$t("settings.agents.unavailable")}</Badge>
    {/if}
    <span class="ml-auto"><SaveStatus {status} error={saveError ?? ""} /></span>
  </div>
  <div class="divide-y divide-border rounded-lg border border-border bg-surface">
    {#if info?.available && info.path}
      <div class="px-4 py-2.5"><p class="font-mono text-[11px] break-all text-muted select-text">{$t("settings.agents.path", { path: info.path })}</p></div>
    {:else if info && !info.available && info.error}
      <div class="px-4 py-2.5"><p class="text-xs break-words text-danger select-text">{info.error}</p></div>
    {/if}
    <SettingsRow label={$t("settings.agents.pathLabel")} hint={$t("settings.agents.pathHint")} for={inputId}>
      <Input
        id={inputId}
        class="w-72 max-w-full font-mono"
        placeholder={$t("settings.agents.override", { kind: $t(`settings.kinds.${kind}`) })}
        aria-label={$t("settings.agents.override", { kind: $t(`settings.kinds.${kind}`) })}
        value={path}
        oninput={(event) => {
          path = event.currentTarget.value;
          saveError = null;
        }}
        onblur={() => void commit()}
        onkeydown={(event) => { if (event.key === "Enter" && !event.isComposing) { event.preventDefault(); void commit(); } }}
      />
    </SettingsRow>
  </div>
</div>
