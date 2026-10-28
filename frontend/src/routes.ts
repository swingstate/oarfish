// The pages of the app. This list is the only place that knows them: the
// navigation, the router and the page title are all built from it. A new page
// adds one entry here (docs/UI.md, "Add a page").
import type { Component } from 'svelte';
import type { IconName } from './lib/icons';
import type { deUi } from './lib/locales/de';
import Device from './pages/Device.svelte';
import Home from './pages/Home.svelte';

export interface Route {
  /** Stable id, used for the current entry. */
  id: string;
  /** The address fragment, for example `#/device`. The first route is also the fallback. */
  path: string;
  /** Key of the label in the UI texts; it is the navigation text and the page title. */
  labelKey: keyof typeof deUi;
  icon: IconName;
  component: Component;
}

export const routes: readonly Route[] = [
  { id: 'home', path: '#/', labelKey: 'navHome', icon: 'home', component: Home },
  { id: 'device', path: '#/device', labelKey: 'navDevice', icon: 'device', component: Device },
];

/** The route for an address fragment. An empty or unknown fragment shows the first route. */
export function routeFromHash(hash: string, list: readonly Route[] = routes): Route {
  return list.find((r) => r.path === hash) ?? list[0]!;
}
