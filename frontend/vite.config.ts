import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Im Dev-Modus leitet Vite alle /api-Aufrufe an das Go-Backend weiter.
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      "/api": { target: process.env.API_URL ?? "http://localhost:8080", changeOrigin: true },
    },
  },
});
