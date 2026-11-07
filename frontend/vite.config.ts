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

// The development scenario mode (src/dev/updateMock.ts) must never reach a
// release: the build fails if its marker is in any output file.
const devMockMarker = 'oarfish-dev-mock';
const noDevMock: Plugin = {
  name: 'oarfish-no-dev-mock',
  apply: 'build',
  generateBundle(_options, bundle) {
    for (const [file, out] of Object.entries(bundle)) {
      const text = out.type === 'chunk' ? out.code : typeof out.source === 'string' ? out.source : new TextDecoder().decode(out.source);
      if (text.includes(devMockMarker) || text.includes('installUpdateMock')) {
        this.error(`${file} contains the development scenario mode; it must not be in a production build`);
      }
    }
  },
};

export default defineConfig({
  plugins: [svelte(), svelteTesting(), keepEmbedDir, noDevMock],
  build: {
    outDir: embedDir,
    emptyOutDir: true,
  },
  server: {
    proxy: {
      '/healthz': 'http://127.0.0.1:8484',
      '/api': 'http://127.0.0.1:8484',
    },
  },
  test: {
    environment: 'jsdom',
    include: ['src/**/*.test.ts'],
    setupFiles: ['src/test-setup.ts'],
  },
});
