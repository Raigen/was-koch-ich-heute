import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// In dev mode, Vite proxies all /api calls to the Go backend.
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      "/api": { target: process.env.API_URL ?? "http://localhost:8080", changeOrigin: true },
    },
  },
});
