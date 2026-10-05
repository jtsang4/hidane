<script lang="ts">
  import DOMPurify from "dompurify";
  import { marked } from "marked";
  import { mount, unmount } from "svelte";
  import { isMermaidBlock, renderMermaid } from "../lib/mermaid.js";
  import { cn } from "../lib/utils.js";
  import CodeCopyButton from "./CodeCopyButton.svelte";

  const BASE = [
    "[&>*:first-child]:mt-0 [&>*:last-child]:mb-0",
    "[&_p]:my-2 [&_p]:leading-relaxed",
    "[&_h1]:mt-4 [&_h1]:mb-2 [&_h1]:text-base [&_h1]:font-semibold",
    "[&_h2]:mt-4 [&_h2]:mb-2 [&_h2]:text-sm [&_h2]:font-semibold",
    "[&_h3]:mt-3 [&_h3]:mb-1 [&_h3]:text-sm [&_h3]:font-semibold",
    "[&_h4]:mt-3 [&_h4]:mb-1 [&_h4]:font-semibold",
    "[&_strong]:font-semibold",
    "[&_ul]:my-2 [&_ul]:list-disc [&_ul]:pl-5",
    "[&_ol]:my-2 [&_ol]:list-decimal [&_ol]:pl-5",
    "[&_li]:my-0.5 [&_li>ul]:my-1 [&_li>ol]:my-1",
    "[&_blockquote]:my-2 [&_blockquote]:border-l-2 [&_blockquote]:border-border [&_blockquote]:pl-3 [&_blockquote]:opacity-80",
    "[&_hr]:my-3 [&_hr]:border-border",
    "[&_a]:underline [&_a]:underline-offset-2",
    "[&_code]:rounded-sm [&_code]:bg-well [&_code]:px-1 [&_code]:py-0.5 [&_code]:font-mono [&_code]:text-code",
    "[&_pre]:overflow-x-auto [&_pre]:rounded-sm [&_pre]:bg-well [&_pre]:p-2",
    "[&_pre_code]:bg-transparent [&_pre_code]:p-0",
    "[&_table]:my-2 [&_table]:block [&_table]:w-full [&_table]:overflow-x-auto [&_table]:border-collapse",
    "[&_th]:border [&_th]:border-border [&_th]:px-2 [&_th]:py-1 [&_th]:text-left [&_th]:font-medium",
    "[&_td]:border [&_td]:border-border [&_td]:px-2 [&_td]:py-1",
    "[&_img]:my-2 [&_img]:max-w-full [&_img]:rounded-sm",
  ].join(" ");

  let { content, streaming = false, class: className = "" }: { content: string; streaming?: boolean; class?: string } = $props();

  let html = $derived(
    streaming ? "" : DOMPurify.sanitize(
      marked.parse(content, { gfm: true, async: false }) as string,
      { ADD_ATTR: ["target", "rel"] },
    ),
  );

  /** Holds a code block and its copy button: the button stays put while a wide block scrolls sideways under it.
      On a touch screen the button is always shown, so the code makes room for it. */
  const CODE_FRAME = "group/code relative my-2 coarse:[&>pre]:min-h-11 coarse:[&>pre]:pr-11";

  /** A drawn ```mermaid block, in the place of its code; on a touch screen it clears the copy button above it. */
  const DIAGRAM = "overflow-x-auto rounded-sm bg-well p-3 coarse:pt-11 [&>svg]:mx-auto [&>svg]:block [&>svg]:h-auto";

  function codeText(pre: HTMLPreElement): string {
    return ((pre.querySelector("code") ?? pre).textContent ?? "").replace(/\n$/, "");
  }

  /** The code stays until the diagram is drawn, and for good when Mermaid cannot draw it. */
  async function drawDiagram(pre: HTMLPreElement): Promise<void> {
    const drawn = await renderMermaid(codeText(pre)).catch(() => null);
    if (!drawn) return;
    const diagram = document.createElement("div");
    diagram.className = DIAGRAM;
    diagram.append(drawn);
    pre.replaceWith(diagram);
  }

  function renderMarkdown(node: HTMLDivElement): () => void {
    node.innerHTML = html;
    const buttons = [...node.querySelectorAll("pre")].map((pre) => {
      const frame = document.createElement("div");
      frame.className = CODE_FRAME;
      pre.replaceWith(frame);
      frame.append(pre);
      if (isMermaidBlock(pre)) void drawDiagram(pre);
      return mount(CodeCopyButton, { target: frame, props: { text: codeText(pre) } });
    });
    return () => {
      for (const button of buttons) void unmount(button);
    };
  }
</script>

{#if streaming}
  <div class={cn("markdown whitespace-pre-wrap break-words", className)}>{content}</div>
{:else}
  <div class={cn("markdown", BASE, "break-words", className)} {@attach renderMarkdown}></div>
{/if}
