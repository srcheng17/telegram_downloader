import { resolve } from 'node:path';
import { defineConfig } from 'vite';

export default defineConfig({
  build: {
    outDir: 'static/dist',
    emptyOutDir: false,
    rollupOptions: {
      input: {
        index: resolve(__dirname, 'frontend/src/index.js'),
        logs: resolve(__dirname, 'frontend/src/logs.js'),
        settings: resolve(__dirname, 'frontend/src/settings.js'),
      },
      output: {
        entryFileNames: '[name].bundle.js',
      },
    },
  },
});
