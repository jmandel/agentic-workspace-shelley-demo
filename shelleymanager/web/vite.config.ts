import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import path from "path";

const managerUrl =
  process.env.MANAGER_URL ||
  process.env.VITE_MANAGER_URL ||
  "http://127.0.0.1:31337";

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: { "@": path.resolve(__dirname, "src") },
  },
  server: {
    port: 5173,
    proxy: {
      "/apis": {
        target: managerUrl,
        changeOrigin: true,
      },
      "/acp": {
        target: managerUrl,
        changeOrigin: true,
        ws: true,
        configure: (proxy) => {
          proxy.on("error", (err, _req, res) => {
            console.error("[proxy] error:", err.message);
            if (res && "writeHead" in res && !res.headersSent) {
              (res as import("http").ServerResponse).writeHead(502, {
                "Content-Type": "text/plain",
              });
              (res as import("http").ServerResponse).end(
                "Proxy error: " + err.message,
              );
            }
          });
        },
      },
      "/workspaces": {
        target: managerUrl,
        changeOrigin: true,
      },
      "/health": {
        target: managerUrl,
        changeOrigin: true,
      },
      "/demo-assets": {
        target: managerUrl,
        changeOrigin: true,
      },
      "/shelley": {
        target: managerUrl,
        changeOrigin: true,
      },
    },
  },
});
