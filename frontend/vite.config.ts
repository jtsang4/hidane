/// <reference types="vitest/config" />
import { mkdirSync, writeFileSync } from "node:fs";
import { join, resolve } from "node:path";
import { defineConfig, type Plugin } from "vite";
import { svelteTesting } from "@testing-library/svelte/vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";
import tailwindcss from "@tailwindcss/vite";

/**
 * The Go binary embeds `frontend/dist`, and `go:embed` refuses an empty or
 * missing directory. `vite build` empties the folder, so the committed
 * placeholder is written back after every build.
 */
function keepDist(): Plugin {
  let outDir = "dist";
  return {
    name: "hidane-keep-dist",
    apply: "build",
    configResolved(config) {
      outDir = resolve(config.root, config.build.outDir);
    },
    closeBundle() {
      mkdirSync(outDir, { recursive: true });
      writeFileSync(join(outDir, ".keep"), "");
    },
  };
}

const backend = "http://localhost:2718";

export default defineConfig({
  plugins: [svelte(), svelteTesting(), tailwindcss(), keepDist()],
  server: {
    port: 2719,
    proxy: {
      "/api": backend,
      "/webhook": backend,
      "/health": backend,
      "/boot.js": backend,
      "/wails": backend,
    },
  },
  test: {
    environment: "jsdom",
    setupFiles: ["test/setup.ts"],
  },
});
