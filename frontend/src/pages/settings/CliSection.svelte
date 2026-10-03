<script lang="ts">
  import { createQuery, useQueryClient } from "@tanstack/svelte-query";
  import { RefreshCw } from "@lucide/svelte";
  import { t } from "../../i18n/index.js";
  import { api, type AgentKind } from "../../lib/api.js";
  import { AGENT_KINDS } from "../../lib/settings.js";
  import AgentRow from "../../components/settings/AgentRow.svelte";
  import Button from "../../components/ui/Button.svelte";

  const queryClient = useQueryClient();
  const settingsQuery = createQuery(() => ({ queryKey: ["settings"], queryFn: () => api.settings() }));
  // Detection runs `<cli> --version` for each CLI; it does not need the default 3s staleness.
  const agentsQuery = createQuery(() => ({ queryKey: ["agents"], queryFn: () => api.agents(), staleTime: 60_000 }));
  let settings = $derived(settingsQuery.data);

  async function save(kind: AgentKind, path: string): Promise<void> {
    const response = await api.saveBinaries({ [kind]: path });
    queryClient.setQueryData(["settings"], response.settings);
    void queryClient.invalidateQueries({ queryKey: ["agents"] });
    void queryClient.invalidateQueries({ queryKey: ["status"] });
  }
</script>

<div class="flex items-start gap-3">
  <p class="min-w-0 flex-1 text-xs text-muted">{$t("settings.agents.hint")}</p>
  <Button variant="secondary" disabled={agentsQuery.isFetching} onclick={() => void agentsQuery.refetch()}>
    <RefreshCw size={14} class={agentsQuery.isFetching ? "animate-spin" : ""} />{$t("settings.agents.redetect")}
  </Button>
</div>
{#if settings}
  {#each AGENT_KINDS as kind (kind)}
    <AgentRow
      {kind}
      info={agentsQuery.data?.agents.find((agent) => agent.kind === kind)}
      detecting={agentsQuery.isFetching}
      savedPath={settings.binaries[kind] ?? ""}
      onsave={(path) => save(kind, path)}
    />
  {/each}
{:else if settingsQuery.isLoading}
  <p class="text-sm text-muted">{$t("common.loading")}</p>
{/if}
