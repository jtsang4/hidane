<script lang="ts">
  import { createQuery } from "@tanstack/svelte-query";
  import { Bell } from "@lucide/svelte";
  import { t } from "../i18n/index.js";
  import { api } from "../lib/api.js";
  import { fmtDateTime } from "../lib/utils.js";
  import { notifyPermission, requestNotifyPermission, type NotifyPermission } from "../lib/notify.js";
  import Badge from "../components/ui/Badge.svelte";
  import Button from "../components/ui/Button.svelte";
  import Card from "../components/ui/Card.svelte";

  let permission = $state<NotifyPermission>(notifyPermission());
  type StatusCard = { label: string; value: string; tone?: "success" | "danger" | "muted" };
  const statusQuery = createQuery(() => ({
    queryKey: ["status"],
    queryFn: () => api.status(),
    refetchInterval: 10_000,
  }));
  let data = $derived(statusQuery.data);
  let heartbeatAge = $derived(data?.lastHeartbeatAt ? Math.round((Date.now() - new Date(data.lastHeartbeatAt).getTime()) / 1000) : null);
  let heartbeatOk = $derived(heartbeatAge !== null && heartbeatAge < 900);
  let cards = $derived<StatusCard[]>(data ? [
    { label: $t("status.latestSeq"), value: String(data.latestSeq) },
    { label: $t("status.triageLag"), value: String(data.triageLag), tone: data.triageLag < 20 ? "success" : "danger" },
    { label: $t("status.lastHeartbeat"), value: data.lastHeartbeatAt ? $t("status.heartbeatAt", { time: fmtDateTime(data.lastHeartbeatAt), s: heartbeatAge ?? 0 }) : $t("status.none"), tone: heartbeatOk ? "success" : "danger" },
    { label: $t("status.openItems"), value: String(data.openWorkItems) },
    { label: $t("status.model"), value: data.model ?? $t("status.none"), tone: data.model?.startsWith("error:") ? "danger" : undefined },
  ] : []);

  async function enableNotifications(): Promise<void> {
    permission = await requestNotifyPermission();
  }
</script>

{#if !data}
  <p class="p-6 text-sm text-muted">{$t("common.loading")}</p>
{:else}
  <div class="space-y-3 p-4">
    <h1 class="text-lg font-semibold">{$t("status.title")}</h1>
    <Card class="space-y-2">
      <div class="flex items-center gap-2"><Bell size={16} class="text-muted" /><span class="text-sm font-medium">{$t("notify.title")}</span></div>
      <p class="text-xs text-muted">{$t("notify.hint")}</p>
      {#if permission === "granted"}<Badge tone="success">{$t("notify.granted")}</Badge>{/if}
      {#if permission === "denied"}<Badge tone="danger">{$t("notify.denied")}</Badge>{/if}
      {#if permission === "unsupported"}<Badge tone="muted">{$t("notify.unsupported")}</Badge>{/if}
      {#if permission === "default"}<Button size="sm" onclick={() => void enableNotifications()}>{$t("notify.enable")}</Button>{/if}
    </Card>
    <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
      {#each cards as card (card.label)}
        <Card>
          <div class="text-xs text-muted">{card.label}</div>
          <div class="mt-1 flex items-center gap-2 text-lg font-semibold">
            {card.value}
            {#if card.tone}<Badge tone={card.tone}>{card.tone === "success" ? $t("status.ok") : $t("status.attention")}</Badge>{/if}
          </div>
        </Card>
      {/each}
    </div>
  </div>
{/if}
