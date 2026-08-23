import type { OutboundImage } from "./api.js";

/** Matches the server's limits; rejecting here gives an immediate reason. */
export const MAX_IMAGES = 4;
export const MAX_IMAGE_BYTES = 6 * 1024 * 1024;

export interface AttachedImage extends OutboundImage {
  /** Object URL for the thumbnail; revoked when the attachment is dropped. */
  previewUrl: string;
  name: string;
}

export function isAcceptable(file: File): boolean {
  return file.type.startsWith("image/") && file.size <= MAX_IMAGE_BYTES;
}

/** Strip the `data:<mime>;base64,` prefix — the API wants raw base64. */
export function stripDataUrl(dataUrl: string): string {
  const comma = dataUrl.indexOf(",");
  return comma === -1 ? dataUrl : dataUrl.slice(comma + 1);
}

export async function readImage(file: File): Promise<AttachedImage> {
  const dataUrl = await new Promise<string>((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result));
    reader.onerror = () => reject(reader.error ?? new Error("read failed"));
    reader.readAsDataURL(file);
  });
  return {
    data: stripDataUrl(dataUrl),
    mimeType: file.type,
    previewUrl: URL.createObjectURL(file),
    name: file.name || "image",
  };
}

/** Files a drop/paste/pick may add, given what is already attached. */
export function acceptableSlice(files: File[], alreadyAttached: number): File[] {
  return files.filter(isAcceptable).slice(0, Math.max(0, MAX_IMAGES - alreadyAttached));
}
