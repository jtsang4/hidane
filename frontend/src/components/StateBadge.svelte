<script lang="ts">
  import { t } from "../i18n/index.js";
  import type { CardState } from "../lib/api.js";
  import { stateTone } from "../lib/board.js";
  import Badge from "./ui/Badge.svelte";

  let { state, class: className = "" }: { state: CardState; class?: string } = $props();
</script>

<Badge class={className} tone={stateTone(state)}>{$t(`task.state.${state}`)}</Badge>
