import { randomBytes } from "node:crypto";

const ALPHABET = "0123456789abcdefghjkmnpqrstvwxyz";

/** Short, sortable-enough, url-safe id like `wi_9f3kq2`. */
export function genId(prefix: string, len = 8): string {
  const bytes = randomBytes(len);
  let out = "";
  for (let i = 0; i < len; i++) {
    out += ALPHABET[(bytes[i] as number) % ALPHABET.length];
  }
  return `${prefix}_${out}`;
}
