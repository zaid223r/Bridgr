import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// The Go API runs on :8080; the dev server proxies to it so the browser sees one
// origin and no CORS setup is needed.
const api = "http://localhost:8080";

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: { "/todos": api, "/openapi.json": api, "/docs": api },
  },
});
