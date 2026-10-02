<script lang="ts">
  import { createQuery } from "@tanstack/svelte-query";
  import { t } from "../i18n/index.js";
  import { api, type RunAs } from "../lib/api.js";
  import { AGENT_LABELS } from "../lib/runAs.js";
  import RunPicker from "./RunPicker.svelte";

  let {
    value,
    onchange,
    scope,
    onconversation,
  }: {
    /** `null` follows the role settings. */
    value: RunAs | null;
    /** A returned promise settles once the change is saved and `value` shows it. */
    onchange: (next: RunAs | null) => unknown;
    /** `new`: the next task this composer creates; `task`: the addressed task, changed at once. */
    scope: "new" | "task";
    /**
     * Changes who answers in the conversation (the Primary role). Left out
     * while a task is addressed: its own Manager answers that message.
     */
    onconversation?: ((next: RunAs) => Promise<void>) | undefined;
  } = $props();

  const settingsQuery = createQuery(() => ({ queryKey: ["settings"], queryFn: () => api.settings(), staleTime: 60_000 }));

  let primary = $derived(settingsQuery.data?.roles.primary ?? null);
  /** The CLI the settings would run the work on; the rest is in Settings. */
  let followLabel = $derived.by(() => {
    const worker = settingsQuery.data?.roles.worker;
    return worker ? AGENT_LABELS[worker.agent] : "";
  });
</script>

<div class="@container">
  <div class="flex flex-wrap items-center gap-1.5" role="group" aria-label={$t("runAs.label")}>
    {#if onconversation && primary}
      <RunPicker kind="conversation" value={primary} onchange={(next) => (next ? onconversation(next) : undefined)} />
    {/if}
    <RunPicker kind={scope} {value} follow={followLabel} {onchange} />
  </div>
</div>
