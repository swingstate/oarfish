import { mount } from 'svelte';
import App from './App.svelte';
import './styles/tokens.css';
import './styles/base.css';

const target = document.getElementById('app');
if (!target) {
  throw new Error('missing #app mount point');
}

// Development scenario mode of the update page and the Activity page
// (docs/DEVELOPMENT.md): only in the Vite development server and only with
// ?mock=<scenario>. Vite replaces import.meta.env.DEV with false in the
// production build, so this branch and the mock modules are not part of it
// (vite.config.ts checks that). A name both pages know (loading, load-error)
// installs both mocks; each answers only its own routes.
const scenario = import.meta.env.DEV ? new URLSearchParams(location.search).get('mock') : null;
if (import.meta.env.DEV && scenario) {
  void Promise.all([import('./dev/updateMock'), import('./dev/servicesMock')]).then(([update, services]) => {
    const forServices = services.installServicesMock(scenario);
    if (!forServices || update.scenarioNames().includes(scenario)) update.installUpdateMock(scenario);
    mount(App, { target });
  });
} else {
  mount(App, { target });
}
