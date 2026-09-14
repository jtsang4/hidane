import { resolve, join } from "node:path";
import { tmpdir } from "node:os";
import { describe, expect, it } from "vitest";
import { ModelRuntime } from "@earendil-works/pi-coding-agent";

describe("deployment pi model catalog", () => {
  it("keeps the configured DeepSeek aliases available without a cache", async () => {
    const runtime = await ModelRuntime.create({
      authPath: join(tmpdir(), `hidane-test-auth-${process.pid}.json`),
      modelsPath: resolve(process.cwd(), "../../deploy/pi-models.json"),
      modelsStorePath: join(tmpdir(), `hidane-test-models-${process.pid}.json`),
      refreshOnCreate: false,
    });

    expect(runtime.getModel("deepseek", "deepseek-flash")).toBeDefined();
    expect(runtime.getModel("deepseek", "deepseek-v4-flash-vision-exp")).toBeDefined();
  });
});
