<script lang="ts" module>
  export interface ComboboxSuggestion {
    value: string;
    /** A display name, shown beside the value when it says something more. */
    label?: string;
  }

  interface Option {
    value: string;
    label: string;
    detail: string;
    typed: boolean;
  }
</script>

<script lang="ts">
  import { Combobox } from "bits-ui";
  import Check from "@lucide/svelte/icons/check";
  import ChevronsUpDown from "@lucide/svelte/icons/chevrons-up-down";
  import { t } from "../../i18n/index.js";
  import { field, fieldSize, popover, popoverItem } from "../../lib/styles.js";
  import { cn } from "../../lib/utils.js";

  /**
   * A text field with suggestions: whatever is typed is a valid value, the
   * list only saves typing. `""` is a value too (the empty choice, e.g. the
   * default model), offered as the first option under `emptyLabel`.
   */
  let {
    value,
    suggestions,
    onchange,
    oninput,
    id,
    label,
    emptyLabel,
    size = "default",
    class: className = "",
  }: {
    value: string;
    suggestions: readonly ComboboxSuggestion[];
    /** A value was chosen or entered (Enter, blur, a click on an option). Callers drop repeats themselves. */
    onchange: (value: string) => void;
    /** Each keystroke, for callers that track an unsaved draft. */
    oninput?: (text: string) => void;
    id?: string | undefined;
    label?: string | undefined;
    /** Names the empty value, in the list and as the field's placeholder. */
    emptyLabel: string;
    size?: "default" | "sm";
    class?: string;
  } = $props();

  /** What the field shows: the value, until typed over. */
  let text = $derived(value);
  /** What was typed since the list opened; empty lists everything. */
  let query = $state("");
  let open = $state(false);

  let options = $derived.by((): Option[] => {
    const typed = query.trim();
    const needle = typed.toLowerCase();
    // Model lists are typed by hand in Settings; a repeat would break the keyed list.
    const unique = [...new Map(suggestions.map((s) => [s.value, s])).values()];
    const rows = unique.map((s) => ({ value: s.value, label: s.value, detail: s.label && s.label !== s.value ? s.label : "", typed: false }));
    if (needle === "") {
      // The current choice first, so the highlight that opening puts on the first option rests on it.
      const current = value.trim();
      const head: Option[] = current === "" ? [] : [rows.find((row) => row.value === current) ?? { value: current, label: current, detail: "", typed: false }];
      const empty: Option = { value: "", label: emptyLabel, detail: "", typed: false };
      return [...head, empty, ...rows.filter((row) => row.value !== current)];
    }
    const matches = rows.filter((row) => row.value.toLowerCase().includes(needle) || row.detail.toLowerCase().includes(needle));
    const exact = matches.find((row) => row.value === typed);
    // What was typed comes first, so Enter takes it as typed rather than the first partial match.
    const head: Option = exact ?? { value: typed, label: typed, detail: "", typed: true };
    return [head, ...matches.filter((row) => row !== exact)];
  });

  function commit(next: string): void {
    text = next;
    query = "";
    onchange(next);
  }
</script>

<!--
  The root's own value stays "", so picking an option, even the one already
  chosen, always reaches `commit`; the check mark follows `value` instead.
-->
<Combobox.Root
  type="single"
  bind:value={() => "", (next) => commit(next)}
  bind:open
  onOpenChange={(isOpen) => {
    if (!isOpen) query = "";
  }}
>
  <div class={cn("relative w-full min-w-0", className)}>
    <Combobox.Input
      {...id ? { id } : {}}
      aria-label={label}
      placeholder={emptyLabel}
      autocomplete="off"
      spellcheck={false}
      class={cn(
        "w-full",
        field,
        fieldSize[size],
        // Empty is a real choice here (the default), so its name reads as a value, not a hint.
        "pr-7 placeholder:text-foreground/80",
      )}
      oninput={(event: Event & { currentTarget: HTMLInputElement }) => {
        text = event.currentTarget.value;
        query = text;
        oninput?.(text);
      }}
      onclick={() => {
        // bits-ui opens only on typing or the chevron; a click on the field should show the choices too.
        open = true;
      }}
      onkeydown={(event: KeyboardEvent) => {
        // An open list takes Enter for its highlighted option, which is what was typed unless the person moved.
        if (event.key === "Enter" && !event.isComposing && !open) {
          event.preventDefault();
          commit(text.trim());
        }
      }}
      onblur={() => commit(text.trim())}
    >
      {#snippet child({ props })}
        <!-- The field always shows `text`: bits-ui would put the picked option's label in it. -->
        <input {...props} value={text} />
      {/snippet}
    </Combobox.Input>
    <Combobox.Trigger
      tabindex={-1}
      aria-label={$t("common.showOptions")}
      class={cn("absolute inset-y-0 right-0 flex items-center justify-center text-muted hover:text-foreground", size === "sm" ? "w-6" : "w-7")}
    >
      <ChevronsUpDown size={12} aria-hidden="true" />
    </Combobox.Trigger>
  </div>
  <Combobox.Portal>
    <Combobox.Content
      sideOffset={4}
      collisionPadding={8}
      class={cn(popover, "max-h-[min(20rem,var(--bits-combobox-content-available-height))] max-w-[min(28rem,var(--bits-combobox-content-available-width))] min-w-[var(--bits-combobox-anchor-width)] overflow-y-auto")}
    >
      {#each options as option (option.value)}
        <Combobox.Item
          value={option.value}
          label={option.label}
          class={cn(popoverItem, "pl-1.5", size === "sm" && "text-xs")}
        >
          <Check size={13} class={cn("shrink-0 text-primary", option.value !== value && "invisible")} aria-hidden="true" />
          <span class={cn("truncate", option.value === "" && "text-muted")}>{option.label}</span>
          {#if option.detail || option.typed}
            <span class="ml-auto shrink-0 pl-3 text-muted">{option.typed ? $t("common.typedValue") : option.detail}</span>
          {/if}
        </Combobox.Item>
      {/each}
    </Combobox.Content>
  </Combobox.Portal>
</Combobox.Root>
