<script lang="ts">
  import { Save } from "@lucide/svelte";
  import { t } from "../../i18n/index.js";
  import type { AgentInfo, AgentKind } from "../../lib/api.js";
  import Badge from "../ui/Badge.svelte";
  import Button from "../ui/Button.svelte";
  import Card from "../ui/Card.svelte";
  import Input from "../ui/Input.svelte";

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
  let dirty = $derived(path.trim() !== savedPath);
  let inputId = $derived(`agent-path-${kind}`);

  async function save(): Promise<void> {
    saving = true;
    try {
      await onsave(path.trim());
    } catch {
      // The caller reports the failure; the typed path stays for correction.
    } finally {
      saving = false;
    }
  }
</script>

<Card class="space-y-2">
  <div class="flex flex-wrap items-center gap-2">
    <h3 class="text-sm font-medium">{$t(`settings.kinds.${kind}`)}</h3>
    {#if detecting && !info}
      <Badge tone="muted">{$t("settings.agents.detecting")}</Badge>
    {:else if info?.available}
      <Badge tone="success">{$t("settings.agents.available")}</Badge>
      {#if info.version}<span class="font-mono text-xs text-muted">{$t("settings.agents.version", { version: info.version })}</span>{/if}
    {:else}
      <Badge tone="danger">{$t("settings.agents.unavailable")}</Badge>
    {/if}
  </div>
  {#if info?.available && info.path}
    <p class="font-mono text-[11px] break-all text-muted">{$t("settings.agents.path", { path: info.path })}</p>
  {:else if info && !info.available && info.error}
    <p class="text-xs break-words text-danger">{info.error}</p>
  {/if}
  <label class="sr-only" for={inputId}>{$t("settings.agents.override", { kind: $t(`settings.kinds.${kind}`) })}</label>
  <div class="flex flex-col gap-2 sm:flex-row">
    <Input
      id={inputId}
      class="font-mono"
      placeholder={$t("settings.agents.override", { kind: $t(`settings.kinds.${kind}`) })}
      bind:value={path}
    />
    <Button size="sm" class="sm:h-9" disabled={saving || !dirty} onclick={() => void save()}>
      <Save size={14} />{$t("settings.agents.save")}
    </Button>
  </div>
</Card>
