// The pages of the app. This list is the only place that knows them: the
// navigation, the router and the page title are all built from it. A new page
// adds one entry here (docs/UI.md, "Add a page").
import type { Component } from 'svelte';
import type { IconName } from './lib/icons';
import type { NavGroup } from './lib/ui';
import type { deUi } from './lib/locales/de';
import Account from './pages/Account.svelte';
import Device from './pages/Device.svelte';
import Dashboard from './pages/Dashboard.svelte';

export interface Route {
  /** Stable id, used for the current entry. */
  id: string;
  /** The address fragment, for example `#/device`. The first route is also the fallback. */
  path: string;
  /** Key of the label in the UI texts; it is the navigation text and the page title. */
  labelKey: keyof typeof deUi;
  icon: IconName;
  /** The navigation group of specification 7.2; none for the entries at the top. */
  group?: NavGroup;
  component: Component;
  /** False for a page reached from somewhere else than the navigation (the account page, from the user menu). */
  nav?: boolean;
}

export const routes: readonly Route[] = [
  { id: 'dashboard', path: '#/', labelKey: 'navDashboard', icon: 'home', component: Dashboard },
  { id: 'device', path: '#/device', labelKey: 'navDevice', icon: 'device', component: Device },
  { id: 'account', path: '#/account', labelKey: 'navAccount', icon: 'user', component: Account, nav: false },
];

/** The route for an address fragment. An empty or unknown fragment shows the first route. */
export function routeFromHash(hash: string, list: readonly Route[] = routes): Route {
  return list.find((r) => r.path === hash) ?? list[0]!;
}
