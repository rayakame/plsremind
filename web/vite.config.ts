import { fileURLToPath } from "node:url";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

const sdk = (path: string) => fileURLToPath(new URL(`../sdk/ts/src/${path}`, import.meta.url));
const hub = process.env.PLSREMIND_DEV_HUB ?? "http://localhost:8080";

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: [
      { find: /^plsremind\/optimistic$/, replacement: sdk("optimistic/index.ts") },
      { find: /^plsremind$/, replacement: sdk("index.ts") },
    ],
  },
  server: {
    proxy: { "/api": { target: hub, ws: true } },
  },
  build: { manifest: true, target: "es2022" },
  test: {
    environment: "happy-dom",
    include: ["test/**/*.test.{ts,tsx}"],
    restoreMocks: true,
  },
});
