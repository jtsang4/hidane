<script lang="ts">
  import { createQuery } from "@tanstack/svelte-query";
  import { Copy, FolderOpen } from "@lucide/svelte";
  import i18n, { t } from "../../i18n/index.js";
  import { api } from "../../lib/api.js";
  import { boot } from "../../lib/boot.js";
  import { copyText, parentDir } from "../../lib/native.js";
  import { pushToast, toastError } from "../../lib/toast.js";
  import SettingsCard from "../../components/settings/SettingsCard.svelte";
  import SettingsRow from "../../components/settings/SettingsRow.svelte";
  import Button from "../../components/ui/Button.svelte";

  const info = boot();
  const settingsQuery = createQuery(() => ({ queryKey: ["settings"], queryFn: () => api.settings() }));
  let dataDir = $derived(settingsQuery.data ? parentDir(settingsQuery.data.path) : "");

  async function copyPath(): Promise<void> {
    try {
      await copyText(dataDir);
      pushToast(i18n.t("settings.about.copied"), "default");
    } catch (error) {
      toastError(error);
    }
  }

  async function reveal(): Promise<void> {
    try {
      await api.openDataDir();
    } catch (error) {
      toastError(error);
    }
  }
</script>

<SettingsCard>
  <div class="flex items-center gap-3 px-4 py-4">
    <img src="/favicon.svg" alt="" class="h-10 w-10" />
    <div>
      <div class="text-sm font-semibold">{$t("common.appName")}</div>
      <div class="text-xs text-muted">{$t(info.desktop ? "settings.about.modeDesktop" : "settings.about.modeBrowser")}</div>
    </div>
  </div>
  <SettingsRow label={$t("settings.about.version")}>
    <span class="font-mono text-sm text-muted select-text">{info.version}</span>
  </SettingsRow>
</SettingsCard>

<SettingsCard title={$t("settings.about.dataDir")} description={$t("settings.about.dataDirHint")}>
  <div class="flex flex-wrap items-center gap-3 px-4 py-3">
    <code class="min-w-0 flex-1 basis-64 font-mono text-xs break-all text-muted select-text">{dataDir || $t("common.loading")}</code>
    <div class="flex shrink-0 gap-2">
      <Button size="sm" variant="outline" disabled={!dataDir} onclick={() => void copyPath()}><Copy size={14} />{$t("settings.about.copyPath")}</Button>
      {#if info.desktop}
        <Button size="sm" variant="outline" onclick={() => void reveal()}><FolderOpen size={14} />{$t("settings.about.reveal")}</Button>
      {/if}
    </div>
  </div>
</SettingsCard>
