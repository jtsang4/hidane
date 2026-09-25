import { describe, expect, it } from "vitest";
import { lastAssistantError, lastAssistantText } from "../src/agents/sdk.js";

describe("reading a model turn's outcome", () => {
  it("reports the provider's error instead of an empty answer", () => {
    const messages = [
      { role: "user", content: "ping" },
      { role: "assistant", content: [], stopReason: "error", errorMessage: '401: {"message":"Invalid API key."}' },
    ];
    expect(lastAssistantText(messages)).toBe("");
    expect(lastAssistantError(messages)).toBe('401: {"message":"Invalid API key."}');
  });

  it("finds no error in a completed answer, even after an earlier failure", () => {
    const messages = [
      { role: "assistant", content: [], stopReason: "error", errorMessage: "rate limited" },
      { role: "user", content: "again" },
      { role: "assistant", content: [{ type: "text", text: "OK" }], stopReason: "stop" },
    ];
    expect(lastAssistantError(messages)).toBeUndefined();
    expect(lastAssistantText(messages)).toBe("OK");
  });

  it("names a failure even when the provider gave no message", () => {
    expect(lastAssistantError([{ role: "assistant", content: [], stopReason: "error" }])).toBe("model request failed");
  });
});
