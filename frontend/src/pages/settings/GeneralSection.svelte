<script lang="ts">
  import { t, language, switchLanguage } from "../../i18n/index.js";
  import { boot } from "../../lib/boot.js";
  import { notifyPermission, requestNotifyPermission, type NotifyPermission } from "../../lib/notify.js";
  import { prefs, setPref } from "../../lib/ui.svelte.js";
  import SettingsCard from "../../components/settings/SettingsCard.svelte";
  import SettingsRow from "../../components/settings/SettingsRow.svelte";
  import Badge from "../../components/ui/Badge.svelte";
  import Button from "../../components/ui/Button.svelte";
  import Select from "../../components/ui/Select.svelte";
  import Switch from "../../components/ui/Switch.svelte";

  let { onsignout }: { onsignout: () => void } = $props();

  const desktop = boot().desktop;
  const needsToken = boot().auth;
  let permission = $state<NotifyPermission>(notifyPermission());

  async function askPermission(): Promise<void> {
    permission = await requestNotifyPermission();
  }
</script>

<SettingsCard title={$t("settings.general.language")}>
  <SettingsRow label={$t("settings.general.languageLabel")} for="settings-language">
    <Select
      id="settings-language"
      class="w-40"
      value={$language}
      options={[{ value: "zh", label: "中文" }, { value: "en", label: "English" }]}
      onchange={(next) => switchLanguage(next === "en" ? "en" : "zh")}
    />
  </SettingsRow>
</SettingsCard>

<SettingsCard title={$t("settings.general.notifications")}>
  <SettingsRow label={$t("settings.general.notify")} hint={$t(desktop ? "settings.general.notifyHintDesktop" : "settings.general.notifyHintBrowser")}>
    <Switch checked={prefs.notify} label={$t("settings.general.notify")} onchange={(value) => setPref("notify", value)} />
  </SettingsRow>
  {#if !desktop}
    <!-- The desktop app notifies through the system; only a browser asks for permission. -->
    <SettingsRow label={$t("notify.permission")} hint={$t("notify.hint")}>
      {#if permission === "granted"}<Badge tone="success">{$t("notify.granted")}</Badge>{/if}
      {#if permission === "denied"}<Badge tone="danger">{$t("notify.denied")}</Badge>{/if}
      {#if permission === "unsupported"}<Badge tone="muted">{$t("notify.unsupported")}</Badge>{/if}
      {#if permission === "default"}<Button variant="secondary" onclick={() => void askPermission()}>{$t("notify.enable")}</Button>{/if}
    </SettingsRow>
  {/if}
  <SettingsRow label={$t(desktop ? "settings.general.badgeDesktop" : "settings.general.badgeBrowser")} hint={$t("settings.general.badgeHint")}>
    <Switch checked={prefs.badge} label={$t(desktop ? "settings.general.badgeDesktop" : "settings.general.badgeBrowser")} onchange={(value) => setPref("badge", value)} />
  </SettingsRow>
</SettingsCard>

{#if needsToken}
  <SettingsCard title={$t("settings.general.account")}>
    <SettingsRow label={$t("token.signOut")} hint={$t("settings.general.signOutHint")}>
      <Button variant="secondary" onclick={onsignout}>{$t("token.signOut")}</Button>
    </SettingsRow>
  </SettingsCard>
{/if}
