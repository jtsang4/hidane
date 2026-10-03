<script lang="ts">
  import { createQuery } from "@tanstack/svelte-query";
  import { t } from "../../i18n/index.js";
  import { api } from "../../lib/api.js";
  import { fmtDateTime } from "../../lib/utils.js";
  import { ROLES } from "../../lib/settings.js";
  import SettingsCard from "../../components/settings/SettingsCard.svelte";
  import SettingsRow from "../../components/settings/SettingsRow.svelte";
  import Badge from "../../components/ui/Badge.svelte";

  type StatusLine = { label: string; value: string; tone?: "success" | "danger" | "muted" | undefined };
  const statusQuery = createQuery(() => ({
    queryKey: ["status"],
    queryFn: () => api.status(),
    refetchInterval: 10_000,
  }));
  let data = $derived(statusQuery.data);
  let heartbeatAge = $derived(data?.lastHeartbeatAt ? Math.round((Date.now() - new Date(data.lastHeartbeatAt).getTime()) / 1000) : null);
  let heartbeatOk = $derived(heartbeatAge !== null && heartbeatAge < 900);
  let runtime = $derived<StatusLine[]>(
    data?.runtime
      ? [
          { label: $t("status.runtimeState"), value: data.runtime.up ? $t("status.ok") : $t("status.runtimeDown"), tone: data.runtime.up ? "success" : "danger" },
          { label: $t("status.turns"), value: data.runtime.activeTurns.length > 0 ? data.runtime.activeTurns.join(", ") : "0" },
          { label: $t("status.pendingMessages"), value: String(data.runtime.pendingMessages) },
          { label: $t("status.workers"), value: $t("status.workersValue", data.runtime.workers) },
        ]
      : [],
  );
  let log = $derived<StatusLine[]>(
    data
      ? [
          { label: $t("status.latestSeq"), value: String(data.latestSeq) },
          { label: $t("status.triageLag"), value: String(data.triageLag), tone: data.triageLag < 20 ? "success" : "danger" },
          {
            label: $t("status.lastHeartbeat"),
            value: data.lastHeartbeatAt ? $t("status.heartbeatAt", { time: fmtDateTime(data.lastHeartbeatAt), s: heartbeatAge ?? 0 }) : $t("status.none"),
            tone: heartbeatOk ? "success" : "danger",
          },
          { label: $t("status.openItems"), value: String(data.openWorkItems) },
          { label: $t("status.model"), value: data.model ?? $t("status.none"), tone: data.model?.startsWith("error:") ? "danger" : undefined },
        ]
      : [],
  );
  let roleRows = $derived(data?.roles ? ROLES.flatMap((role) => (data.roles?.[role] ? [{ role, ...data.roles[role] }] : [])) : []);
</script>

{#snippet line(item: StatusLine)}
  <SettingsRow label={item.label}>
    <span class="flex items-center gap-2 text-sm tabular-nums select-text">
      <!-- A value that is itself the verdict is shown as the badge alone. -->
      {#if item.tone && item.value === $t("status.ok")}
        <Badge tone={item.tone}>{item.value}</Badge>
      {:else}
        {item.value}
        {#if item.tone}<Badge tone={item.tone}>{item.tone === "success" ? $t("status.ok") : $t("status.attention")}</Badge>{/if}
      {/if}
    </span>
  </SettingsRow>
{/snippet}

{#if !data}
  <p class="flex items-center gap-2 text-sm text-muted"><span class="size-3 animate-spin rounded-full border-[1.5px] border-muted border-t-transparent"></span>{$t("common.loading")}</p>
{:else}
  {#if runtime.length > 0}
    <SettingsCard title={$t("status.runtime")}>
      {#each runtime as item (item.label)}{@render line(item)}{/each}
    </SettingsCard>
  {/if}
  <SettingsCard title={$t("status.logTitle")}>
    {#each log as item (item.label)}{@render line(item)}{/each}
  </SettingsCard>
  {#if data.agents && data.agents.length > 0}
    <SettingsCard title={$t("status.agents")}>
      <ul class="divide-y divide-border">
        {#each data.agents as agent (agent.kind)}
          <li class="flex flex-wrap items-center gap-2 px-3.5 py-2.5 text-sm">
            <span class="font-medium">{$t(`settings.kinds.${agent.kind}`)}</span>
            {#if agent.available}
              <Badge tone="success">{$t("settings.agents.available")}</Badge>
              {#if agent.version}<span class="font-mono text-xs text-muted">{agent.version}</span>{/if}
            {:else}
              <Badge tone="danger">{$t("settings.agents.unavailable")}</Badge>
              {#if agent.error}<span class="text-xs break-words text-danger">{agent.error}</span>{/if}
            {/if}
          </li>
        {/each}
      </ul>
    </SettingsCard>
  {/if}
  {#if roleRows.length > 0}
    <SettingsCard title={$t("status.roles")}>
      <ul class="divide-y divide-border">
        {#each roleRows as row (row.role)}
          <li class="flex flex-wrap items-baseline gap-x-2 px-3.5 py-2.5 text-sm">
            <span class="font-medium">{$t(`settings.roles.${row.role}`)}</span>
            <span class="text-muted">{$t(`settings.kinds.${row.agent}`)} · {row.provider || $t("settings.ownLogin")} · <span class="font-mono">{row.model || $t("status.defaultModel")}</span></span>
          </li>
        {/each}
      </ul>
    </SettingsCard>
  {/if}
{/if}
