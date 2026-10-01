<script lang="ts">
  import DOMPurify from "dompurify";
  import { marked } from "marked";
  import { cn } from "../lib/utils.js";

  const BASE = [
    "[&>*:first-child]:mt-0 [&>*:last-child]:mb-0",
    "[&_p]:my-2 [&_p]:leading-relaxed",
    "[&_h1]:mt-4 [&_h1]:mb-2 [&_h1]:text-base [&_h1]:font-bold",
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
    "[&_code]:rounded [&_code]:bg-black/25 [&_code]:px-1 [&_code]:py-0.5 [&_code]:font-mono [&_code]:text-[0.85em]",
    "[&_pre]:my-2 [&_pre]:overflow-x-auto [&_pre]:rounded [&_pre]:bg-black/30 [&_pre]:p-2",
    "[&_pre_code]:bg-transparent [&_pre_code]:p-0",
    "[&_table]:my-2 [&_table]:block [&_table]:w-full [&_table]:overflow-x-auto [&_table]:border-collapse",
    "[&_th]:border [&_th]:border-border [&_th]:px-2 [&_th]:py-1 [&_th]:text-left [&_th]:font-semibold",
    "[&_td]:border [&_td]:border-border [&_td]:px-2 [&_td]:py-1",
    "[&_img]:my-2 [&_img]:max-w-full [&_img]:rounded",
  ].join(" ");

  let { content, class: className = "" }: { content: string; class?: string } = $props();

  let html = $derived(
    DOMPurify.sanitize(
      marked.parse(content, { gfm: true, async: false }) as string,
      { ADD_ATTR: ["target", "rel"] },
    ),
  );

  function renderMarkdown(node: HTMLDivElement): void {
    node.innerHTML = html;
  }
</script>

<div class={cn("markdown", BASE, "break-words", className)} {@attach renderMarkdown}></div>
