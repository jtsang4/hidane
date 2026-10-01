<script lang="ts">
  import { createMutation, createQuery, useQueryClient } from "@tanstack/svelte-query";
  import { Plus } from "@lucide/svelte";
  import { t } from "../i18n/index.js";
  import { api, ApiError } from "../lib/api.js";
  import { matchesItem } from "../lib/search.js";
  import { focusHref, navigate } from "../lib/router.svelte.js";
  import { pushToast } from "../lib/toast.js";
  import Badge from "../components/ui/Badge.svelte";
  import Button from "../components/ui/Button.svelte";
  import Card from "../components/ui/Card.svelte";
  import Input from "../components/ui/Input.svelte";
  import Textarea from "../components/ui/Textarea.svelte";
  import Time from "../components/Time.svelte";

  const queryClient = useQueryClient();
  let all = $state(false);
  let query = $state("");
  let creating = $state(false);
  let title = $state("");
  let brief = $state("");
  let repo = $state("");

  const itemsQuery = createQuery(() => ({
    queryKey: ["items", all],
    queryFn: () => api.workItems(all),
  }));
  let items = $derived(itemsQuery.data?.items ?? []);
  let running = $derived(new Set(itemsQuery.data?.running ?? []));
  let shown = $derived(query.trim() ? items.filter((item) => matchesItem(item, query)) : items);

  const create = createMutation(() => ({
    mutationFn: () =>
      api.createWorkItem({
        title: title.trim(),
        ...(brief.trim() ? { brief: brief.trim() } : {}),
        ...(repo.trim() ? { repo: repo.trim() } : {}),
      }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["items"] });
      title = "";
      brief = "";
      repo = "";
      creating = false;
    },
    onError: (error) => pushToast(error instanceof ApiError ? error.message : String(error)),
  }));

  function openItem(id: string, event: MouseEvent): void {
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    navigate(focusHref(id));
  }
</script>

<div class="space-y-3 p-4">
  <div class="flex items-center justify-between gap-2">
    <h1 class="text-lg font-semibold">{$t("items.title")}</h1>
    <div class="flex shrink-0 items-center gap-2">
      {#if query.trim()}<span class="text-xs text-muted">{$t("items.matched", { n: shown.length, total: items.length })}</span>{/if}
      <Button variant="outline" size="sm" onclick={() => (all = !all)}>{$t(all ? "items.onlyOpen" : "items.showArchived")}</Button>
      {#if !creating}<Button size="sm" onclick={() => (creating = true)}><Plus size={16} />{$t("items.new")}</Button>{/if}
    </div>
  </div>
  {#if creating}
    <Card class="space-y-2">
      <Input autofocus bind:value={title} placeholder={$t("items.form.title")} />
      <Textarea rows={2} bind:value={brief} placeholder={$t("items.form.brief")} />
      <Input bind:value={repo} placeholder={$t("items.form.repo")} />
      <div class="flex justify-end gap-2">
        <Button variant="outline" size="sm" onclick={() => (creating = false)}>{$t("common.cancel")}</Button>
        <Button size="sm" disabled={create.isPending || title.trim().length === 0} onclick={() => create.mutate()}>{$t("items.form.create")}</Button>
      </div>
    </Card>
  {/if}
  <Input bind:value={query} placeholder={$t("items.search")} />
  {#each shown as item (item.id)}
    <a href={focusHref(item.id)} class="block" onclick={(event) => openItem(item.id, event)}>
      <Card class="transition-colors hover:bg-surface-2">
        <div class="flex items-center justify-between gap-2">
          <div class="min-w-0">
            <div class="font-medium">{item.title}</div>
            <div class="mt-1 text-xs text-muted">{item.id} · {$t("items.updated")} <Time iso={item.updatedAt} /></div>
          </div>
          <div class="flex shrink-0 items-center gap-2">
            {#if running.has(item.id)}
              <Badge tone="default"><span class="mr-1 inline-block h-1.5 w-1.5 animate-pulse rounded-full bg-primary"></span>{$t("items.working")}</Badge>
            {/if}
            <Badge tone={item.status === "open" ? "success" : "muted"}>{item.status}</Badge>
          </div>
        </div>
      </Card>
    </a>
  {/each}
  {#if items.length === 0}<p class="pt-8 text-center text-sm text-muted">{$t("items.empty")}</p>{/if}
  {#if items.length > 0 && shown.length === 0}<p class="pt-8 text-center text-sm text-muted">{$t("items.noMatch")}</p>{/if}
</div>
