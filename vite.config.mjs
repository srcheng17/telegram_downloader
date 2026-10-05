import { resolve } from 'node:path';
import { defineConfig } from 'vite';

export default defineConfig({
  build: {
    outDir: 'web/static/dist',
    emptyOutDir: true,
    rollupOptions: {
      input: {
        app: resolve(import.meta.dirname, 'frontend/src/app.js'),
        index: resolve(import.meta.dirname, 'frontend/src/index.js'),
        logs: resolve(import.meta.dirname, 'frontend/src/logs.js'),
        komga: resolve(import.meta.dirname, 'frontend/src/komga.js'),
        settings: resolve(import.meta.dirname, 'frontend/src/settings.js'),
      },
      output: {
        entryFileNames: '[name].bundle.js',
      },
    },
  },
});
