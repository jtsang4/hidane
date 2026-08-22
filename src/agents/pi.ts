/** Extract the first JSON object from model output (fenced block preferred). */
export function extractJson<T = Record<string, unknown>>(text: string): T | null {
  const fenced = text.match(/```(?:json)?\s*(\{[\s\S]*?\})\s*```/);
  const candidates: string[] = [];
  if (fenced?.[1]) candidates.push(fenced[1]);
  const start = text.indexOf("{");
  if (start >= 0) {
    let depth = 0;
    let inStr = false;
    let esc = false;
    for (let i = start; i < text.length; i++) {
      const ch = text[i];
      if (esc) {
        esc = false;
        continue;
      }
      if (ch === "\\") {
        esc = true;
        continue;
      }
      if (ch === '"') inStr = !inStr;
      if (inStr) continue;
      if (ch === "{") depth++;
      if (ch === "}") {
        depth--;
        if (depth === 0) {
          candidates.push(text.slice(start, i + 1));
          break;
        }
      }
    }
  }
  for (const c of candidates) {
    try {
      return JSON.parse(c) as T;
    } catch {
      // try next candidate
    }
  }
  return null;
}
