<script lang="ts">
  import { useQueryClient } from "@tanstack/svelte-query";
  import { Dialog } from "bits-ui";
  import { LoaderCircle } from "@lucide/svelte";
  import i18n, { t } from "../i18n/index.js";
  import { api } from "../lib/api.js";
  import { focusHref, navigate } from "../lib/router.svelte.js";
  import { errorText } from "../lib/settings.js";
  import { pushToast } from "../lib/toast.js";
  import Button from "./ui/Button.svelte";
  import Input from "./ui/Input.svelte";
  import Textarea from "./ui/Textarea.svelte";

  let { onclose }: { onclose: () => void } = $props();

  const queryClient = useQueryClient();
  let title = $state("");
  let brief = $state("");
  let repo = $state("");
  let pending = $state(false);
  let error = $state<string | null>(null);
  let titleInput = $state<HTMLInputElement | null>(null);

  async function create(): Promise<void> {
    if (!title.trim() || pending) return;
    pending = true;
    error = null;
    try {
      const { item } = await api.createWorkItem({
        title: title.trim(),
        ...(brief.trim() ? { brief: brief.trim() } : {}),
        ...(repo.trim() ? { repo: repo.trim() } : {}),
      });
      void queryClient.invalidateQueries({ queryKey: ["items"] });
      void queryClient.invalidateQueries({ queryKey: ["board"] });
      pushToast(i18n.t("newTask.created"), "default");
      onclose();
      navigate(focusHref(item.id));
    } catch (failure) {
      error = errorText(failure);
    } finally {
      pending = false;
    }
  }

  function onkeydown(event: KeyboardEvent): void {
    if (event.key === "Enter" && !event.isComposing && (event.metaKey || event.ctrlKey || event.target instanceof HTMLInputElement)) {
      event.preventDefault();
      void create();
    }
  }
</script>

<!-- Mounted while open by App; any way of closing it (Esc, a click outside) is Cancel. -->
<Dialog.Root bind:open={() => true, (open) => { if (!open) onclose(); }}>
  <Dialog.Portal>
    <Dialog.Overlay class="fixed inset-0 z-[60] bg-black/40" />
    <Dialog.Content
      class="fixed top-[12vh] left-1/2 z-[60] w-[520px] max-w-[calc(100%-2rem)] -translate-x-1/2 rounded-xl border border-border bg-surface p-5 shadow-2xl outline-none"
      onOpenAutoFocus={(event) => {
        event.preventDefault();
        titleInput?.focus();
      }}
      {onkeydown}
    >
      <Dialog.Title class="text-sm font-semibold">{$t("newTask.title")}</Dialog.Title>
      <div class="mt-4 space-y-3">
        <label class="block space-y-1 text-xs text-muted">
          <span>{$t("newTask.name")}</span>
          <Input bind:ref={titleInput} bind:value={title} placeholder={$t("items.form.title")} />
        </label>
        <label class="block space-y-1 text-xs text-muted">
          <span>{$t("newTask.brief")}</span>
          <Textarea rows={3} bind:value={brief} placeholder={$t("items.form.brief")} />
        </label>
        <label class="block space-y-1 text-xs text-muted">
          <span>{$t("newTask.repo")}</span>
          <Input class="font-mono" bind:value={repo} placeholder={$t("items.form.repo")} />
        </label>
        {#if error}<p class="text-xs break-words text-danger" role="alert">{error}</p>{/if}
      </div>
      <div class="mt-5 flex items-center justify-end gap-2">
        <span class="mr-auto text-[11px] text-muted">{$t("newTask.hint")}</span>
        <Button variant="outline" size="sm" onclick={onclose}>{$t("common.cancel")}</Button>
        <Button size="sm" disabled={pending || title.trim().length === 0} onclick={() => void create()}>
          {#if pending}<LoaderCircle size={14} class="animate-spin" />{/if}{$t("items.form.create")}
        </Button>
      </div>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>
