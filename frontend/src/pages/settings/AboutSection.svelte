<script lang="ts">
  import { createQuery } from "@tanstack/svelte-query";
  import { Copy, FolderOpen } from "@lucide/svelte";
  import i18n, { t } from "../../i18n/index.js";
  import { api } from "../../lib/api.js";
  import { boot } from "../../lib/boot.js";
  import { copyText, parentDir } from "../../lib/native.js";
  import { pushToast, toastError } from "../../lib/toast.js";
  import BrandMark from "../../components/BrandMark.svelte";
  import PathText from "../../components/PathText.svelte";
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
  <div class="flex items-center gap-3 px-3.5 py-3">
    <div class="relative" aria-hidden="true">
      <div class="absolute inset-0 rounded-full bg-primary/25 blur-md"></div>
      <div class="relative grid size-10 place-items-center rounded-xl border border-border bg-linear-to-b from-surface-2 to-surface shadow-popover"><BrandMark size={20} /></div>
    </div>
    <div>
      <div class="text-base font-semibold tracking-tight">{$t("common.appName")}</div>
      <div class="text-xs text-muted">{$t(info.desktop ? "settings.about.modeDesktop" : "settings.about.modeBrowser")}</div>
    </div>
  </div>
  <SettingsRow label={$t("settings.about.version")}>
    <span class="font-mono text-sm text-muted select-text">{info.version}</span>
  </SettingsRow>
</SettingsCard>

<SettingsCard title={$t("settings.about.dataDir")} description={$t("settings.about.dataDirHint")}>
  <div class="flex flex-wrap items-center gap-3 px-3.5 py-2.5">
    <code class="min-w-0 flex-1 basis-64 font-mono text-xs break-words text-muted select-text">{#if dataDir}<PathText path={dataDir} />{:else}{$t("common.loading")}{/if}</code>
    <div class="flex shrink-0 gap-2">
      <Button variant="secondary" disabled={!dataDir} onclick={() => void copyPath()}><Copy size={14} />{$t("settings.about.copyPath")}</Button>
      {#if info.desktop}
        <Button variant="secondary" onclick={() => void reveal()}><FolderOpen size={14} />{$t("settings.about.reveal")}</Button>
      {/if}
    </div>
  </div>
</SettingsCard>
