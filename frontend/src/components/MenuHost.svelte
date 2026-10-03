<script lang="ts">
  import { tick } from "svelte";
  import { DropdownMenu } from "bits-ui";
  import { closeMenu, menuState, type MenuRequest } from "../lib/contextMenu.svelte.js";
  import { popover, popoverItem } from "../lib/styles.js";
  import { cn } from "../lib/utils.js";

  let content = $state<HTMLElement | null>(null);

  function finish(request: MenuRequest | null, choice: string | null): void {
    // Choosing an item also closes the menu; the second call finds it already gone. While the
    // menu is torn down `request` (an {@const}) already reads null.
    if (request === null || menuState.current !== request) return;
    closeMenu();
    if (choice === null) {
      request.returnFocus?.focus();
      return;
    }
    // After the menu is gone, so a confirm dialog the action opens gets focus.
    void tick().then(() => request.onselect(choice));
  }

  function dismiss(): void {
    const request = menuState.current;
    if (request) finish(request, null);
  }

  /** The point the request names, as something Floating UI can anchor to. */
  function anchorAt(request: MenuRequest) {
    return { getBoundingClientRect: () => new DOMRect(request.x, request.y, 0, 0) };
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
    <DropdownMenu.Root bind:open={() => true, (open) => { if (!open) finish(request, null); }}>
      <DropdownMenu.Portal>
        <DropdownMenu.Content
          bind:ref={content}
          customAnchor={anchorAt(request)}
          side="bottom"
          align={request.align}
          collisionPadding={8}
          loop
          aria-label={request.label}
          class={cn(popover, "z-[66] min-w-44")}
          onOpenAutoFocus={(event) => {
            // A right click opens it too, and the keyboard must be able to take over at once.
            event.preventDefault();
            void tick().then(() => content?.querySelector<HTMLElement>("[role=menuitem]")?.focus());
          }}
          onCloseAutoFocus={(event) => event.preventDefault()}
        >
          {#each request.items as item, index (item.id)}
            {@const Icon = item.icon}
            <!-- A destructive action that closes the menu is set apart from the rest. -->
            {#if item.danger && index > 0 && index === request.items.length - 1}<DropdownMenu.Separator class="-mx-1 my-1 h-px bg-border" />{/if}
            <DropdownMenu.Item
              textValue={item.label}
              class={cn(
                popoverItem,
                item.danger ? "text-danger" : "text-foreground",
              )}
              onSelect={() => finish(request, item.id)}
            >
              {#if Icon}<Icon size={14} class="shrink-0 text-muted" />{/if}
              <span class="truncate">{item.label}</span>
            </DropdownMenu.Item>
          {/each}
        </DropdownMenu.Content>
      </DropdownMenu.Portal>
    </DropdownMenu.Root>
  {/key}
{/if}
