/// <reference types="vitest/config" />
import { defineConfig } from "vite";
import { svelteTesting } from "@testing-library/svelte/vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  plugins: [svelte(), svelteTesting(), tailwindcss()],
  server: {
    port: 2719,
    proxy: {
      "/api": "http://localhost:2718",
      "/webhook": "http://localhost:2718",
      "/health": "http://localhost:2718",
    },
  },
  test: {
    environment: "jsdom",
    setupFiles: ["test/setup.ts"],
  },
});
