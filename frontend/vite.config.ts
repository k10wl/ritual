import { defineConfig } from "vite";
import wails from "@wailsio/runtime/plugins/vite";

export default defineConfig({
  plugins: [wails("./bindings")],
  build: {
    rollupOptions: {
      input: {
        main: "index.html",
        logs: "logs.html",
      },
    },
  },
});
