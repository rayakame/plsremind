import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    projects: [
      { test: { name: "unit", include: ["test/*.test.ts"] } },
      {
        test: {
          name: "hub",
          include: ["test/hub/*.test.ts"],
          globalSetup: ["test/hub/setup.ts"],
          testTimeout: 30_000,
          hookTimeout: 30_000,
          fileParallelism: false,
        },
      },
    ],
  },
});
