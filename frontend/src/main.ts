import { mount } from 'svelte';
import App from './App.svelte';
import { pageVersion } from './lib/version';
import './styles/tokens.css';
import './styles/base.css';

const target = document.getElementById('app');
if (!target) {
  throw new Error('missing #app mount point');
}

function start(el: HTMLElement): void {
  // Remember the version that served this page, so the update page can load the
  // new page after a GUI update (task N0, lib/version.ts).
  void pageVersion();
  mount(App, { target: el });
}

// Development scenario mode of the update page, the Activity page, the network card and the Gerät page
// (docs/DEVELOPMENT.md): only in the Vite development server and only with
// ?mock=<scenario>. Vite replaces import.meta.env.DEV with false in the
// production build, so this branch and the mock modules are not part of it
// (vite.config.ts checks that). A name both pages know (loading, load-error)
// installs both mocks; each answers only its own routes.
const scenario = import.meta.env.DEV ? new URLSearchParams(location.search).get('mock') : null;
if (import.meta.env.DEV && scenario) {
  void Promise.all([import('./dev/updateMock'), import('./dev/servicesMock'), import('./dev/metricsMock')]).then(([update, services, metrics]) => {
    // The scenarios of the network card and the Gerät page (task N0) have their own names.
    if (!metrics.installMetricsMock(scenario)) {
      const forServices = services.installServicesMock(scenario);
      if (!forServices || update.scenarioNames().includes(scenario)) update.installUpdateMock(scenario);
    }
    start(target);
  });
} else {
  start(target);
}
