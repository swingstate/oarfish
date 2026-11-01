import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen } from '@testing-library/svelte';
import { createRawSnippet, type Snippet } from 'svelte';
import AlertList from './AlertList.svelte';
import AppShell from './AppShell.svelte';
import Badge from './Badge.svelte';
import Card from './Card.svelte';
import ConnectionIndicator from './ConnectionIndicator.svelte';
import EmptyState from './EmptyState.svelte';
import KeyValueList from './KeyValueList.svelte';
import Meter from './Meter.svelte';
import PageHeader from './PageHeader.svelte';
import Sparkline from './Sparkline.svelte';
import SummaryStrip from './SummaryStrip.svelte';
import { t } from '../lib/i18n';
import { iconPaths } from '../lib/icons';
import { criticalTemperatureAlert, genericModeAlert } from '../lib/metrics.fixtures';
import type { StreamState } from '../lib/stream';
import type { NavItem } from '../lib/ui';

afterEach(cleanup);

const text = (value: string): Snippet => createRawSnippet(() => ({ render: () => `<span>${value}</span>` }));

describe('components', () => {
  it('Badge: five tones, the text is the content', () => {
    for (const tone of ['neutral', 'success', 'information', 'warning', 'critical'] as const) {
      const { container, unmount } = render(Badge, { tone, children: text(tone) });
      const badge = container.querySelector('.badge')!;
      expect(badge.getAttribute('data-tone')).toBe(tone);
      expect(badge.textContent).toBe(tone);
      unmount();
    }
  });

  it('Meter: a native progress with an accessible name and a value', () => {
    render(Meter, { value: 42, label: 'Auslastung', level: 'relevant' });
    const meter = screen.getByRole('progressbar', { name: 'Auslastung' });
    expect(meter.tagName).toBe('PROGRESS');
    expect(meter.getAttribute('value')).toBe('42');
    expect(meter.getAttribute('data-level')).toBe('relevant');
  });

  it('Sparkline: a described image with two or more values, a text note otherwise', () => {
    const { unmount } = render(Sparkline, { values: [1, 2, 3, null, 5], min: 0, max: 10, label: 'Verlauf, zuletzt 5' });
    expect(screen.getByRole('img', { name: 'Verlauf, zuletzt 5' })).toBeTruthy();
    unmount();
    render(Sparkline, { values: [1], min: 0, max: 10, label: 'x' });
    expect(screen.queryByRole('img')).toBeNull();
    expect(screen.getByText(t.sparkNoData)).toBeTruthy();
  });

  it('KeyValueList: a definition list; a missing value is a neutral placeholder; a note is in parentheses', () => {
    const { container } = render(KeyValueList, {
      items: [
        { label: 'Eins', value: '1' },
        { label: 'Zwei', value: null },
        { label: 'Drei', value: null, placeholder: t.unknownValue },
        { label: 'Vier', value: 'a', note: 'b', mono: true },
      ],
    });
    expect(container.querySelectorAll('dl > div > dt')).toHaveLength(4);
    expect(container.querySelectorAll('dl > div > dd')).toHaveLength(4);
    const dd = Array.from(container.querySelectorAll('dd'));
    expect(dd[1]!.textContent?.trim()).toBe(t.unavailableValue);
    expect(dd[2]!.textContent?.trim()).toBe(t.unknownValue);
    expect(dd[3]!.textContent?.replace(/\s+/g, ' ').trim()).toBe('a (b)');
    expect(dd[3]!.classList.contains('num')).toBe(true);
  });

  it('ConnectionIndicator: every state has its own text, and it is not a live region', () => {
    const expected: Record<StreamState, string> = {
      live: t.connIndicatorLive,
      connecting: t.connIndicatorConnecting,
      reconnecting: t.connIndicatorReconnecting,
      stale: t.connIndicatorStale,
      paused: t.connIndicatorPaused,
    };
    expect(new Set(Object.values(expected)).size).toBe(5);
    for (const [state, label] of Object.entries(expected)) {
      const { container, unmount } = render(ConnectionIndicator, { state: state as StreamState });
      expect(container.textContent?.trim()).toBe(label);
      expect(container.querySelector('[aria-live], [role]')).toBeNull();
      expect(container.querySelector('.dot')!.getAttribute('aria-hidden')).toBe('true');
      unmount();
    }
  });

  it('EmptyState: title, optional description, optional live role on the title only', () => {
    render(EmptyState, { title: 'Titel', description: 'Beschreibung', role: 'alert' });
    expect(screen.getByRole('alert').textContent).toBe('Titel');
    expect(screen.getByText('Beschreibung')).toBeTruthy();
  });

  it('Card: a labelled section; stale dims and says Veraltet', () => {
    const { container, rerender } = render(Card, { id: 'x', title: 'Titel', children: text('Inhalt') });
    expect(screen.getByRole('region', { name: 'Titel' })).toBeTruthy();
    expect(container.querySelector('h2')!.id).toBe('x-heading');
    expect(screen.queryByText(t.staleBadge)).toBeNull();
    rerender({ id: 'x', title: 'Titel', stale: true, children: text('Inhalt') });
    expect(screen.getByText(t.staleBadge)).toBeTruthy();
  });

  it('SummaryStrip: a group, not a section; a long value is cut with its full text as title', () => {
    const long = 'n'.repeat(90);
    const { container } = render(SummaryStrip, {
      label: 'Überblick',
      items: [
        { label: 'Name', value: long, truncate: true, testid: 'name' },
        { label: 'Leer', value: null },
        { label: 'Status', badge: { text: 'Kompatibel', tone: 'information' }, link: { href: '#/x', text: 'Mehr' } },
      ],
    });
    expect(container.querySelector('section')).toBeNull();
    expect(screen.getByRole('group', { name: 'Überblick' })).toBeTruthy();
    expect(screen.getByTestId('name').getAttribute('title')).toBe(long);
    expect(screen.getByText(t.unknownValue)).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Mehr' }).getAttribute('href')).toBe('#/x');
  });

  it('AlertList: the only live region, with the empty state inside it', () => {
    const { container, rerender } = render(AlertList, { alerts: [] });
    expect(container.querySelectorAll('[aria-live]')).toHaveLength(1);
    expect(screen.getByText(t.alertsNone)).toBeTruthy();
    rerender({ alerts: [criticalTemperatureAlert, genericModeAlert] });
    expect(container.querySelectorAll('[aria-live]')).toHaveLength(1);
    expect(container.querySelectorAll('li')).toHaveLength(2);
  });

  it('AlertList: an empty list from old values has no all-clear mark and a neutral icon', () => {
    const iconOf = (container: HTMLElement) => Array.from(container.querySelectorAll('.none svg path')).map((p) => p.getAttribute('d'));
    const { container, rerender } = render(AlertList, { alerts: [], stale: false });
    expect(iconOf(container)).toEqual(iconPaths.success);
    expect(container.querySelector('.none')!.classList.contains('old')).toBe(false);
    rerender({ alerts: [], stale: true });
    expect(screen.getByText(t.alertsNoneStale)).toBeTruthy();
    expect(iconOf(container)).toEqual(iconPaths.clock);
    expect(iconOf(container)).not.toEqual(iconPaths.success);
    expect(container.querySelector('.none')!.classList.contains('old')).toBe(true);
    expect(container.querySelectorAll('[aria-live]')).toHaveLength(1);
  });

  it('PageHeader: the one h1, with an optional aside', () => {
    render(PageHeader, { title: 'Seite', aside: text('Rechts') });
    expect(screen.getByRole('heading', { level: 1, name: 'Seite' })).toBeTruthy();
    expect(screen.getByText('Rechts')).toBeTruthy();
  });
});

describe('AppShell', () => {
  const items: NavItem[] = [
    { id: 'a', href: '#/', label: 'Eins', icon: 'home' },
    { id: 'b', href: '#/b', label: 'Zwei', icon: 'device' },
    { id: 'c', href: '#/c', label: 'Drei', icon: 'chip' }, // a later page needs only an entry
  ];

  it('renders one link per entry, in order, and marks the current one', () => {
    render(AppShell, { items, current: 'b', children: text('Seite') });
    const nav = screen.getByRole('navigation', { name: t.navLabel });
    const links = Array.from(nav.querySelectorAll('a'));
    expect(links.map((a) => a.textContent?.trim())).toEqual(['Eins', 'Zwei', 'Drei']);
    expect(links.map((a) => a.getAttribute('href'))).toEqual(['#/', '#/b', '#/c']);
    expect(links.map((a) => a.getAttribute('aria-current'))).toEqual([null, 'page', null]);
  });

  it('has the landmarks header, nav and main, and a skip link as the first focusable element', () => {
    const { container } = render(AppShell, { items, current: 'a', children: text('Seite') });
    expect(screen.getByRole('banner')).toBeTruthy();
    expect(screen.getAllByRole('navigation')).toHaveLength(1);
    expect(screen.getAllByRole('main')).toHaveLength(1);
    const first = container.querySelector('a, button, summary, input, [tabindex="0"]')!;
    expect(first.textContent).toBe(t.skipLink);
    expect(first.getAttribute('href')).toBe('#main');
    expect(screen.getByRole('main').id).toBe('main');
  });

  it('moves the focus to the content without changing the address when the skip link is used', async () => {
    location.hash = '#/b';
    render(AppShell, { items, current: 'b', children: text('Seite') });
    screen.getByText(t.skipLink).click();
    expect(document.activeElement).toBe(screen.getByRole('main'));
    expect(location.hash).toBe('#/b');
    location.hash = '';
  });
});

// jsdom does not lay out. The browser check at 360, 1280, 1920 and 2560 px (docs/UI.md, "Layout rules") is a
// headless Chrome script outside the repository; this pins the rule it checks.
describe('AppShell layout rule', () => {
  const css = readFileSync(join(import.meta.dirname, 'AppShell.svelte'), 'utf8').replace(/\/\*[\s\S]*?\*\//g, '');
  const rule = (selector: string) => new RegExp(`(?:^|\\n)\\s*${selector.replace('.', '\\.')}\\s*\\{([^}]*)\\}`).exec(css)?.[1] ?? '';

  it('puts the content next to the sidebar, bounded by --content-max, never centred', () => {
    const content = rule('.content');
    expect(content).toMatch(/max-width:\s*var\(--content-max\)/);
    expect(content).not.toMatch(/margin(-inline(-start)?)?\s*:[^;]*auto/);
    expect(content).not.toMatch(/(?:justify-self|place-self)\s*:\s*center/);
  });

  it('has no centring of the content anywhere in the shell', () => {
    expect(css).not.toMatch(/margin(-inline)?\s*:\s*(0\s+)?auto/);
  });
});
