/**
 * Incremental extraction of the user-visible reply from a role's JSON answer.
 *
 * Role charters require a single JSON object and nothing else, so the raw token
 * stream reads `{"action":"reply","reply":"…`. Forwarding it verbatim would
 * print JSON at the reader. This walks the accumulating buffer and yields only
 * the decoded contents of the `reply` string — the one field a human is meant
 * to see.
 *
 * Decisions that carry no `reply` (new_work_item, route_to_work_item) therefore
 * stream nothing at all, which is correct: their user-visible text is composed
 * by the runtime afterwards, not by the model.
 */

const SIMPLE_ESCAPES: Record<string, string> = {
  '"': '"',
  "\\": "\\",
  "/": "/",
  b: "\b",
  f: "\f",
  n: "\n",
  r: "\r",
  t: "\t",
};

/**
 * The `reply` key, not a `reply` value. Requiring the colon is what keeps this
 * off the `"action":"reply"` that precedes it in every charter example.
 */
const REPLY_KEY = /"reply"\s*:\s*"/;

const HEX4 = /^[0-9a-fA-F]{4}$/;

/** Decode as much of the reply string as has unambiguously arrived. */
function decodeSoFar(raw: string): string {
  const match = REPLY_KEY.exec(raw);
  if (!match) return "";
  let i = match.index + match[0].length;
  let out = "";
  while (i < raw.length) {
    const ch = raw[i];
    if (ch === undefined || ch === '"') break;
    if (ch !== "\\") {
      out += ch;
      i += 1;
      continue;
    }
    // A trailing backslash is the front half of an escape still in flight;
    // decoding it now would emit a character the model did not write.
    const esc = raw[i + 1];
    if (esc === undefined) break;
    if (esc === "u") {
      const hex = raw.slice(i + 2, i + 6);
      if (!HEX4.test(hex)) break;
      out += String.fromCharCode(Number.parseInt(hex, 16));
      i += 6;
      continue;
    }
    out += SIMPLE_ESCAPES[esc] ?? esc;
    i += 2;
  }
  // Never hand out half a surrogate pair: the two units can land in separate
  // chunks, and a lone one renders as a replacement character downstream.
  const last = out.charCodeAt(out.length - 1);
  if (out.length > 0 && last >= 0xd800 && last <= 0xdbff) return out.slice(0, -1);
  return out;
}

/** Feed the next raw chunk; returns the newly decoded reply text, if any. */
export type ReplyExtractor = (chunk: string) => string;

export function createReplyExtractor(): ReplyExtractor {
  let raw = "";
  let emitted = 0;
  return (chunk) => {
    raw += chunk;
    const decoded = decodeSoFar(raw);
    if (decoded.length <= emitted) return "";
    const delta = decoded.slice(emitted);
    emitted = decoded.length;
    return delta;
  };
}
