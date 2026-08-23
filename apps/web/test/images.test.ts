import { describe, expect, it } from "vitest";
import {
  acceptableSlice,
  isAcceptable,
  stripDataUrl,
  MAX_IMAGES,
  MAX_IMAGE_BYTES,
} from "../src/lib/images.js";

function file(name: string, type: string, size: number): File {
  const f = new File(["x"], name, { type });
  Object.defineProperty(f, "size", { value: size });
  return f;
}

describe("image attachments", () => {
  it("accepts images within the size limit only", () => {
    expect(isAcceptable(file("a.png", "image/png", 1000))).toBe(true);
    expect(isAcceptable(file("a.pdf", "application/pdf", 1000))).toBe(false);
    expect(isAcceptable(file("big.png", "image/png", MAX_IMAGE_BYTES + 1))).toBe(false);
  });

  it("caps the batch at the server's limit, counting what is already attached", () => {
    const many = Array.from({ length: 10 }, (_, i) => file(`${i}.png`, "image/png", 10));
    expect(acceptableSlice(many, 0)).toHaveLength(MAX_IMAGES);
    expect(acceptableSlice(many, MAX_IMAGES - 1)).toHaveLength(1);
    expect(acceptableSlice(many, MAX_IMAGES)).toHaveLength(0);
  });

  it("drops non-images from a mixed selection instead of failing the whole drop", () => {
    const mixed = [
      file("a.png", "image/png", 10),
      file("b.txt", "text/plain", 10),
      file("c.jpg", "image/jpeg", 10),
    ];
    expect(acceptableSlice(mixed, 0).map((f) => f.name)).toEqual(["a.png", "c.jpg"]);
  });

  it("strips the data URL prefix — the API wants raw base64", () => {
    expect(stripDataUrl("data:image/png;base64,AAAB")).toBe("AAAB");
    expect(stripDataUrl("AAAB")).toBe("AAAB");
  });
});
