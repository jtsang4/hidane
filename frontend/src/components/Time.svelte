<script lang="ts">
  import { onMount } from "svelte";
  import { fmtDateTime, fmtRelative } from "../lib/utils.js";

  let { iso, class: className = "" }: { iso: string; class?: string } = $props();
  let now = $state(Date.now());

  onMount(() => {
    const timer = window.setInterval(() => {
      now = Date.now();
    }, 30_000);
    const onVisible = () => {
      if (document.visibilityState === "visible") now = Date.now();
    };
    document.addEventListener("visibilitychange", onVisible);
    return () => {
      window.clearInterval(timer);
      document.removeEventListener("visibilitychange", onVisible);
    };
  });
</script>

<time dateTime={iso} title={fmtDateTime(iso)} class={className}>{fmtRelative(iso, now)}</time>
