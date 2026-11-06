// Every test starts like a first visit by a German browser with an empty store,
// so the tests that look for German texts do not depend on jsdom's default
// language. A test that needs another browser overrides navigator.languages.
import { beforeEach } from 'vitest';
import { initLang } from './lib/lang.svelte';

Object.defineProperty(navigator, 'languages', { value: ['de-DE', 'de'], configurable: true });
Object.defineProperty(navigator, 'language', { value: 'de-DE', configurable: true });

beforeEach(() => {
  try {
    localStorage.clear();
  } catch {
    // A test that made the store throw restores it itself.
  }
  initLang();
});
