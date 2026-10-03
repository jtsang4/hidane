<script lang="ts">
  import { createQuery, useQueryClient } from "@tanstack/svelte-query";
  import { t } from "../../i18n/index.js";
  import { api, type Role, type RoleConfig } from "../../lib/api.js";
  import { ROLES } from "../../lib/settings.js";
  import RoleRow from "../../components/settings/RoleRow.svelte";

  const queryClient = useQueryClient();
  const settingsQuery = createQuery(() => ({ queryKey: ["settings"], queryFn: () => api.settings() }));
  let settings = $derived(settingsQuery.data);

  /** One role per request: the server merges, so rows saving at once cannot overwrite each other. */
  async function save(role: Role, config: RoleConfig): Promise<void> {
    const response = await api.saveRoles({ [role]: config });
    queryClient.setQueryData(["settings"], response.settings);
    void queryClient.invalidateQueries({ queryKey: ["status"] });
  }
</script>

<p class="text-xs text-muted">{$t("settings.roles.hint")}</p>
{#if settingsQuery.isLoading}
  <p class="flex items-center gap-2 text-sm text-muted"><span class="size-3 animate-spin rounded-full border-[1.5px] border-muted border-t-transparent"></span>{$t("common.loading")}</p>
{:else if settings}
  {#each ROLES as role (role)}
    <RoleRow
      {role}
      config={settings.roles[role]}
      providers={settings.providers}
      onsave={(config) => save(role, config)}
      ontest={() => api.testAgent(role)}
    />
  {/each}
{/if}
