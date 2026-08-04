import adapter from "@sveltejs/adapter-node";
import { sveltekit } from "@sveltejs/kit/vite";
import { defineConfig } from "vite";

// The Go API the dev server proxies /api and /auth to. Matches ORACLE_PORT's default.
const apiTarget = "http://localhost:8080";

export default defineConfig({
  server: {
    proxy: {
      "/api": { target: apiTarget, changeOrigin: true },
      "/auth": { target: apiTarget, changeOrigin: true },
    },
  },
  plugins: [
    sveltekit({
      compilerOptions: {
        // Force runes mode for the project, except for libraries. Can be removed in svelte 6.
        runes: ({ filename }) => (filename.split(/[/\\]/).includes("node_modules") ? undefined : true),
      },

      // adapter-node produces a standalone Node server (build/handler.js) that the
      // Docker image wraps with a small proxy for /api, /auth, and /health.
      adapter: adapter(),
    }),
  ],
});
