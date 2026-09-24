import { mkdir, readFile, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { config } from "../config.js";
import { genId } from "../kernel/ids.js";

export interface InboundImage {
  data: string;
  mimeType: string;
}

export interface StoredImage {
  path: string;
  mimeType: string;
}

const EXT: Record<string, string> = {
  "image/png": "png",
  "image/jpeg": "jpg",
  "image/gif": "gif",
  "image/webp": "webp",
};

/**
 * Images wait in files until the turn that reads them. Messages are delivered
 * through the log, and base64 in an event payload would bloat every query that
 * touches it — the log stores the reference, the file stores the bytes.
 */
export async function storeImages(images: InboundImage[]): Promise<StoredImage[]> {
  if (images.length === 0) return [];
  const day = new Date().toISOString().slice(0, 10);
  const dir = join(config.home, "inbox", day);
  await mkdir(dir, { recursive: true });
  const stored: StoredImage[] = [];
  for (const image of images) {
    const path = join(dir, `${genId("img", 8)}.${EXT[image.mimeType] ?? "bin"}`);
    await writeFile(path, Buffer.from(image.data, "base64"));
    stored.push({ path, mimeType: image.mimeType });
  }
  return stored;
}

export function storedImagesOf(payload: Record<string, unknown>): StoredImage[] {
  const raw = payload["images"];
  if (!Array.isArray(raw)) return [];
  return raw.filter(
    (i): i is StoredImage =>
      typeof (i as StoredImage)?.path === "string" && typeof (i as StoredImage).mimeType === "string",
  );
}

export async function loadImages(stored: StoredImage[]): Promise<InboundImage[]> {
  const images: InboundImage[] = [];
  for (const image of stored) {
    try {
      images.push({ data: (await readFile(image.path)).toString("base64"), mimeType: image.mimeType });
    } catch {
      // A missing file is reported by the absence of the image, not a failed turn.
    }
  }
  return images;
}
