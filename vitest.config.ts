import { defineConfig } from "vitest/config";
import { join } from "node:path";

export default defineConfig({
  test: {
    fileParallelism: false,
    setupFiles: ["test/setup.ts"],
    env: {
      DATABASE_URL: "postgres://hidane:hidane@localhost:2716/hidane_test",
      HIDANE_HOME: join(import.meta.dirname, ".test-home"),
    },
  },
});
