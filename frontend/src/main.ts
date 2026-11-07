import { mount } from 'svelte';
import App from './App.svelte';
import './styles/tokens.css';
import './styles/base.css';

const target = document.getElementById('app');
if (!target) {
  throw new Error('missing #app mount point');
}

// Development scenario mode of the update page (docs/DEVELOPMENT.md): only in
// the Vite development server and only with ?mock=<scenario>. Vite replaces
// import.meta.env.DEV with false in the production build, so this branch and
// the mock module are not part of it (vite.config.ts checks that).
const scenario = import.meta.env.DEV ? new URLSearchParams(location.search).get('mock') : null;
if (import.meta.env.DEV && scenario) {
  void import('./dev/updateMock').then((m) => {
    m.installUpdateMock(scenario);
    mount(App, { target });
  });
} else {
  mount(App, { target });
}
