import { describe, expect, it } from 'vitest';
import { de, en } from './lib/i18n';
import { icons } from './lib/icons.fake';
import { routeFromHash, routes, type Route } from './routes';

describe('navigation list', () => {
  it('lists Home first and then Gerät in the navigation, and the account page outside it', () => {
    const nav = routes.filter((r) => r.nav !== false);
    expect(nav.map((r) => r.id)).toEqual(['home', 'device']);
    expect(nav.map((r) => de[r.labelKey])).toEqual(['Home', 'Gerät']);
    expect(routes.filter((r) => r.nav === false).map((r) => r.id)).toEqual(['account']);
    expect(routeFromHash('#/account').id).toBe('account');
  });

  it('has unique ids and paths, a text in both languages, and a known icon for every entry', () => {
    expect(new Set(routes.map((r) => r.id)).size).toBe(routes.length);
    expect(new Set(routes.map((r) => r.path)).size).toBe(routes.length);
    for (const r of routes) {
      expect(de[r.labelKey], r.id).toBeTruthy();
      expect(en[r.labelKey], r.id).toBeTruthy();
      expect(icons, r.id).toContain(r.icon);
      expect(r.path.startsWith('#/'), r.id).toBe(true);
    }
  });

  it('resolves an address to its page, and anything else to the first page', () => {
    expect(routeFromHash('#/device').id).toBe('device');
    expect(routeFromHash('#/').id).toBe('home');
    expect(routeFromHash('').id).toBe('home');
    expect(routeFromHash('#main').id).toBe('home');
    expect(routeFromHash('#/nowhere').id).toBe('home');
  });

  it('finds a page added to the list without any other change', () => {
    const extra: Route = { ...routes[1]!, id: 'system', path: '#/system' };
    expect(routeFromHash('#/system', [...routes, extra]).id).toBe('system');
  });
});
