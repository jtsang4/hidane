import { beforeEach, describe, expect, it } from "vitest";
import { authHeaders, eventStreamUrl, getToken, setToken } from "../src/lib/api.js";

beforeEach(() => localStorage.clear());

describe("api client auth plumbing", () => {
  it("builds bearer headers from the stored token", () => {
    expect(authHeaders()).toEqual({});
    setToken("abc123");
    expect(getToken()).toBe("abc123");
    expect(authHeaders()).toEqual({ authorization: "Bearer abc123" });
  });

  it("passes the token to the SSE url as query (EventSource cannot set headers)", () => {
    expect(eventStreamUrl()).toBe("/api/events/stream");
    setToken("s3cret/+=");
    expect(eventStreamUrl()).toBe(
      `/api/events/stream?token=${encodeURIComponent("s3cret/+=")}`,
    );
  });
});
