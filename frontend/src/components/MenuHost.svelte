<script lang="ts">
  import { tick } from "svelte";
  import { closeMenu, menuState, type MenuRequest } from "../lib/contextMenu.svelte.js";
  import { cn } from "../lib/utils.js";

  let menu = $state<HTMLDivElement | undefined>();
  let active = $state(0);
  let position = $state<{ left: number; top: number } | null>(null);

  function finish(request: MenuRequest, choice: string | null): void {
    closeMenu();
    position = null;
    if (choice === null) {
      request.returnFocus?.focus();
      return;
    }
    // After the menu is gone, so a confirm dialog the action opens gets focus.
    void tick().then(() => request.onselect(choice));
  }

  /** Place it where asked, then keep it inside the window. Transparent, not hidden, until then: hidden cannot take focus. */
  function place(request: MenuRequest) {
    return (node: HTMLDivElement) => {
      const rect = node.getBoundingClientRect();
      const margin = 8;
      let left = request.align === "end" ? request.x - rect.width : request.x;
      let top = request.y;
      if (left + rect.width > window.innerWidth - margin) left = window.innerWidth - rect.width - margin;
      if (top + rect.height > window.innerHeight - margin) top = Math.max(margin, request.y - rect.height);
      position = { left: Math.max(margin, left), top: Math.max(margin, top) };
      active = 0;
      node.querySelector<HTMLElement>("[role=menuitem]")?.focus();
    };
  }

  function onkeydown(event: KeyboardEvent, request: MenuRequest): void {
    const count = request.items.length;
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      active = (active + (event.key === "ArrowDown" ? 1 : -1) + count) % count;
      menu?.querySelectorAll<HTMLElement>("[role=menuitem]")[active]?.focus();
    } else if (event.key === "Home" || event.key === "End") {
      event.preventDefault();
      active = event.key === "Home" ? 0 : count - 1;
      menu?.querySelectorAll<HTMLElement>("[role=menuitem]")[active]?.focus();
    } else if (event.key === "Escape") {
      event.preventDefault();
      event.stopPropagation();
      finish(request, null);
    } else if (event.key === "Tab") {
      event.preventDefault();
      finish(request, null);
    }
  }

  function dismiss(): void {
    const request = menuState.current;
    if (request) finish(request, null);
  }
</script>

<svelte:window onresize={dismiss} onblur={dismiss} />

{#if menuState.current}
  {@const request = menuState.current}
  {#key request.key}
    <!-- The backdrop takes the click that closes the menu, so it never reaches what is under it. -->
    <div
      class="fixed inset-0 z-[65]"
      role="presentation"
      onmousedown={(event) => { event.preventDefault(); dismiss(); }}
      oncontextmenu={(event) => { event.preventDefault(); dismiss(); }}
      onwheel={dismiss}
    ></div>
    <div
      bind:this={menu}
      role="menu"
      aria-label={request.label}
      tabindex="-1"
      class={cn("fixed z-[66] min-w-48 rounded-lg border border-border bg-surface p-1 shadow-xl outline-none", !position && "opacity-0")}
      style:left={`${position?.left ?? request.x}px`}
      style:top={`${position?.top ?? request.y}px`}
      onkeydown={(event) => onkeydown(event, request)}
      {@attach place(request)}
    >
      {#each request.items as item, index (item.id)}
        {@const Icon = item.icon}
        <button
          role="menuitem"
          tabindex={index === active ? 0 : -1}
          class={cn(
            "flex w-full items-center gap-2 rounded-md px-2.5 py-1.5 text-left text-sm outline-none focus:bg-surface-2",
            item.danger ? "text-danger" : "text-foreground",
          )}
          onmouseenter={(event) => { active = index; event.currentTarget.focus(); }}
          onclick={() => finish(request, item.id)}
        >
          {#if Icon}<Icon size={14} class="shrink-0 opacity-80" />{/if}
          <span class="truncate">{item.label}</span>
        </button>
      {/each}
    </div>
  {/key}
{/if}
