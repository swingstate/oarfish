import { writeFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { defineConfig, type Plugin } from 'vitest/config';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import { svelteTesting } from '@testing-library/svelte/vite';

// Production output goes straight into the Go embed directory.
const embedDir = resolve(import.meta.dirname, '../backend/internal/web/dist');

// emptyOutDir wipes the tracked .gitkeep that keeps the Go embed pattern valid
// on a fresh checkout, so put it back after every build.
const keepEmbedDir: Plugin = {
  name: 'oarfish-keep-embed-dir',
  apply: 'build',
  closeBundle() {
    writeFileSync(resolve(embedDir, '.gitkeep'), '');
  },
};

export default defineConfig({
  plugins: [svelte(), svelteTesting(), keepEmbedDir],
  build: {
    outDir: embedDir,
    emptyOutDir: true,
  },
  server: {
    proxy: {
      '/healthz': 'http://127.0.0.1:8484',
    },
  },
  test: {
    environment: 'jsdom',
    include: ['src/**/*.test.ts'],
  },
});
