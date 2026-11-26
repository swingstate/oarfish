// The Activity page (G7.4): every designed state, the filter and "Mehr
// anzeigen", the inline action panel and its focus rules, the action flow with
// polling, every result sentence, both languages, untrusted text, and the
// development scenario mode (docs/UI.md, "Activity page"; ADR 0011 B2, B3, B4).
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte';
import Activity from './Activity.svelte';
import { de, en, fmt, t } from '../lib/i18n';
import { setLang } from '../lib/lang.svelte';
import { forgetSession, onSessionLost } from '../lib/session';
import {
  brokerMissingList,
  done,
  failedList,
  hostileList,
  longList,
  noAction,
  noFailedList,
  row,
  running,
  sampleList,
  transitionList,
} from '../lib/services.fixtures';
import { activeStates, type ActionState, type ServicesList } from '../lib/services';
import { errorText, resultText, serviceCodes } from '../lib/serviceErrors';
import { installServicesMock, servicesScenarioNames } from '../dev/servicesMock';

// ---- a fake server with plain response objects (no streams, so fake timers stay exact) ----

type Reply = { status: number; body?: unknown } | 'network';
type Handler = (body: Record<string, unknown>) => Reply | Promise<Reply>;

let routes: Record<string, Handler>;
let calls: { key: string; body: Record<string, unknown> }[];

const ok = (body: unknown, status = 200): Reply => ({ status, body });
const err = (status: number, code: string, extra: Record<string, unknown> = {}): Reply => ({
  status,
  body: { error: { code, message: 'English server text that must not be shown', recovery: 'x', correlation_id: '0123456789abcdef', ...extra } },
});

function respond(r: Reply): Response {
  if (r === 'network') throw new TypeError('offline');
  return {
    ok: r.status >= 200 && r.status < 300,
    status: r.status,
    headers: new Headers(),
    json: async () => {
      if (r.body === undefined) throw new SyntaxError('no body');
      return structuredClone(r.body);
    },
  } as unknown as Response;
}

function serve(table: Record<string, Handler>): void {
  routes = table;
  calls = [];
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string, init: RequestInit = {}) => {
      const key = `${init.method ?? 'GET'} ${url}`;
      const body = typeof init.body === 'string' ? (JSON.parse(init.body) as Record<string, unknown>) : {};
      calls.push({ key, body });
      const h = routes[key];
      return respond(h ? await h(body) : err(404, 'not_found'));
    }),
  );
}

const LIST = 'GET /api/v1/services';
const LAST = 'GET /api/v1/services/action';
const POST = 'POST /api/v1/services/action';
const count = (key: string) => calls.filter((c) => c.key === key).length;

async function flush(ms = 0): Promise<void> {
  await vi.advanceTimersByTimeAsync(ms);
  await vi.advanceTimersByTimeAsync(0);
}

async function open(list: ServicesList | (() => Reply), extra: Record<string, Handler> = {}): Promise<void> {
  serve({ [LIST]: typeof list === 'function' ? list : () => ok(list), [LAST]: () => ok(noAction), ...extra });
  render(Activity);
  await flush();
}

const byTestId = (id: string) => screen.getByTestId(id);
const text = (el: Element | null) => el?.textContent?.replace(/\s+/g, ' ').trim() ?? '';
const rowOf = (name: string) => screen.getAllByTestId('act-row').find((r) => r.getAttribute('data-name') === name)!;
const live = () => text(byTestId('act-live'));

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'setInterval', 'clearInterval', 'Date'] });
  vi.setSystemTime(new Date('2026-11-25T10:00:00Z'));
});
afterEach(() => {
  cleanup();
  forgetSession();
  setLang('de');
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

// ---- every state of docs/UI.md, "Activity page" ------------------------------------

describe('activity page states', () => {
  it('state loading: says that the services are being read, as a status', async () => {
    serve({ [LIST]: () => new Promise<Reply>(() => {}), [LAST]: () => ok(noAction) });
    render(Activity);
    await flush();
    expect(screen.getByRole('heading', { level: 1, name: t.activityTitle })).toBeTruthy();
    expect(screen.getByRole('status').textContent).toContain(t.actLoading);
  });

  it('state load error: names the failure in a message area, with a retry that reads again', async () => {
    let fail = true;
    await open(() => (fail ? 'network' : ok(sampleList())));
    expect(text(screen.getByRole('alert'))).toContain(t.actLoadError);
    expect(screen.getByText(t.actLoadErrorHint)).toBeTruthy();
    fail = false;
    await fireEvent.click(screen.getByRole('button', { name: t.dashboardRetry }));
    await flush();
    expect(screen.getAllByTestId('act-row').length).toBe(sampleList().services.length);
  });

  it('state load error: an answer of an unknown shape, a read failure and a server error are load errors too', async () => {
    for (const reply of [ok({ schema_version: 2 }), ok({ ...sampleList(), services: [{ name: 'x' }] }), err(503, 'services_read_failed'), err(500, 'x')]) {
      await open(() => reply);
      expect(text(screen.getByRole('alert'))).toContain(t.actLoadError);
      cleanup();
    }
  });

  it('state systemd unavailable: its own sentence, no list, no action', async () => {
    await open(() => err(503, 'services_unavailable'));
    expect(text(screen.getByRole('status'))).toBe(t.actSystemdMissing);
    expect(text(byTestId('activity'))).toContain(t.actSystemdMissingHint);
    expect(screen.queryByTestId('act-rows')).toBeNull();
    expect(screen.queryByRole('alert')).toBeNull();
  });

  it('state list: one h1, the card heading, every row with name, state in words, start mode in words and description', async () => {
    await open(sampleList());
    expect(screen.getAllByRole('heading', { level: 1 }).length).toBe(1);
    expect(screen.getByRole('heading', { level: 2, name: t.actServicesHeading })).toBeTruthy();
    const rows = screen.getAllByTestId('act-row');
    expect(rows.map((r) => r.getAttribute('data-name'))).toEqual(sampleList().services.map((s) => s.name));
    const app = rowOf('demo-app.service');
    expect(app.querySelector('code')?.textContent).toBe('demo-app.service');
    expect(text(within(app).getByTestId('act-state'))).toBe('aktiv');
    expect(text(within(app).getByTestId('act-mode'))).toBe('Startmodus: Automatisch');
    expect(text(app)).toContain('Demo application server');
    expect(text(within(rowOf('demo-idle.service')).getByTestId('act-mode'))).toBe('Startmodus: Manuell');
    expect(text(rowOf('demo-idle.service'))).toContain(t.actNoDescription);
    expect(text(within(rowOf('example-web.service')).getByTestId('act-mode'))).toBe('Startmodus: Unbekannt');
    expect(text(within(rowOf('example-run.service')).getByTestId('act-state'))).toBe('startet');
    const l = sampleList();
    // The sample has one activating and one reloading row: named as their own group.
    expect(text(byTestId('act-counts'))).toBe(fmt('actCountsTransition', { ...l.counts, transition: 2 }));
    expect(text(byTestId('act-read-at'))).toMatch(/^Gelesen: /);
    // The state is a word in a badge: never colour alone.
    expect(within(app).getByTestId('act-state').getAttribute('data-tone')).toBe('success');
  });

  it('state list with failed services: the failed state in words and a critical badge', async () => {
    await open(failedList());
    const broken = rowOf('demo-broken.service');
    expect(text(within(broken).getByTestId('act-state'))).toBe('fehlgeschlagen');
    expect(within(broken).getByTestId('act-state').getAttribute('data-tone')).toBe('critical');
    expect(text(byTestId('act-counts'))).toContain('3 fehlgeschlagen');
  });

  // G7-FIX1: on the device the line read "156 Dienste, davon 41 aktiv, 0 fehlgeschlagen und 114 inaktiv" (sum 155).
  it('state list in transition: one activating row is its own group, the line adds up, and the filter shows it', async () => {
    const l = transitionList();
    await open(l);
    expect(l.counts).toEqual({ total: 9, active: 4, failed: 0, inactive: 4 });
    expect(text(byTestId('act-counts'))).toBe('9 Dienste, davon 4 aktiv, 0 fehlgeschlagen, 4 inaktiv und 1 im Übergang.');
    await fireEvent.click(screen.getByLabelText(t['actFilter.transition']));
    expect(screen.getAllByTestId('act-row').map((r) => r.dataset.name)).toEqual(['example-run.service']);
    // The other filters keep their meaning: exactly their own state.
    for (const f of ['active', 'failed', 'inactive'] as const) {
      await fireEvent.click(screen.getByLabelText(t[`actFilter.${f}`]));
      const states = screen.queryAllByTestId('act-row').map((r) => l.services.find((s) => s.name === r.dataset.name)!.active_state);
      expect(states.length).toBe(l.counts[f]);
      expect(states.every((s) => s === f)).toBe(true);
    }
    await fireEvent.click(screen.getByLabelText(t['actFilter.all']));
    expect(screen.getAllByTestId('act-row').length).toBe(l.counts.total);
  });

  it('the count line adds up for every active state of the closed set', async () => {
    for (const s of activeStates) {
      cleanup();
      const rows = [row('demo-a.service', 'active', 'enabled'), row('demo-b.service', s, 'enabled'), row('demo-c.service', 'inactive', 'disabled')];
      const counts = {
        total: 3,
        active: rows.filter((r) => r.active_state === 'active').length,
        failed: rows.filter((r) => r.active_state === 'failed').length,
        inactive: rows.filter((r) => r.active_state === 'inactive').length,
      };
      await open({ schema_version: 1, read_at: sampleList().read_at, broker_available: true, counts, services: rows });
      const nums = text(byTestId('act-counts')).match(/\d+/g)!.map(Number);
      expect(nums[0], s).toBe(3);
      expect(nums.slice(1).reduce((a, b) => a + b, 0), s).toBe(3);
      if (['active', 'failed', 'inactive'].includes(s)) expect(text(byTestId('act-counts')), s).not.toContain('Übergang');
      else expect(text(byTestId('act-counts')), s).toContain('1 im Übergang');
      // The filter shows the row under exactly one group.
      let seen = 0;
      for (const f of ['active', 'failed', 'inactive', 'transition'] as const) {
        await fireEvent.click(screen.getByLabelText(t[`actFilter.${f}`]));
        if (screen.queryAllByTestId('act-row').some((r) => r.dataset.name === 'demo-b.service')) seen++;
      }
      expect(seen, s).toBe(1);
    }
  });

  it('marks critical and restricted rows: the kind, the restart-only note, the denied note without a button', async () => {
    await open(sampleList());
    expect(text(within(rowOf('docker.service')).getByTestId('act-critical'))).toBe('Kritischer Dienst: Container');
    expect(text(within(rowOf('ssh.service')).getByTestId('act-critical'))).toBe('Kritischer Dienst: SSH');
    expect(text(within(rowOf('ssh.service')).getByTestId('act-restart-only'))).toBe(`${t.actRestartOnlyRow}. ${t.actRestartOnlyNote}`);
    for (const name of ['dbus.service', 'systemd-journald.service', 'getty@tty1.service', 'oarfish.service']) {
      const r = rowOf(name);
      expect(text(within(r).getByTestId('act-denied'))).toBe(`${t.actDeniedRow}. ${t.actDeniedNote}`);
      expect(within(r).queryByRole('button')).toBeNull();
    }
    expect(t.actDeniedNote).toBe('Diesen Dienst verwaltet Oarfish nicht. Er wird für Oarfish selbst oder für den Systemstart gebraucht.');
    expect(t.actRestartOnlyNote).toBe('Diesen Dienst können Sie nur neu starten. Ein Stopp würde den Zugang zum Gerät trennen.');
  });

  it('state broker unavailable: the list stays, a notice says so, no action button works', async () => {
    await open(brokerMissingList());
    expect(text(byTestId('act-broker-missing'))).toBe(t['svcErr.unavailable']);
    expect(screen.getAllByTestId('act-row').length).toBe(brokerMissingList().services.length);
    await fireEvent.click(within(rowOf('demo-app.service')).getByTestId('act-actions'));
    await flush();
    const panel = byTestId('act-panel');
    expect(text(within(panel).getByTestId('act-panel-unavailable'))).toBe(t['svcErr.unavailable']);
    for (const b of within(panel).getAllByRole('button').filter((b) => b.dataset.testid?.startsWith('act-do-'))) {
      expect((b as HTMLButtonElement).disabled).toBe(true);
      await fireEvent.click(b);
    }
    await flush();
    expect(count(POST)).toBe(0);
  });

  it('shows names, descriptions and unknown words as text only, never as markup', async () => {
    await open(hostileList());
    const r = rowOf('demo-markup.service');
    expect(r.querySelector('img')).toBeNull();
    expect(r.querySelector('b')).toBeNull();
    expect(text(r)).toContain('<img src=x onerror="window.__pwned=1">Demo <b>bold</b>');
    expect((window as unknown as { __pwned?: number }).__pwned).toBeUndefined();
    // A state, a start mode and a kind this version does not know: neutral words.
    const odd = rowOf('demo-state.service');
    expect(text(within(odd).getByTestId('act-state'))).toBe(t.unknownValue);
    expect(within(odd).getByTestId('act-state').getAttribute('data-tone')).toBe('neutral');
    expect(text(within(odd).getByTestId('act-mode'))).toBe(`Startmodus: ${t.unknownValue}`);
    expect(text(within(odd).getByTestId('act-critical'))).toBe('Kritischer Dienst: Sonstiges');
  });

  it('says so when the system reports no services', async () => {
    await open({ ...sampleList(), services: [], counts: { total: 0, active: 0, failed: 0, inactive: 0 } });
    expect(text(byTestId('act-empty'))).toBe(t.actNoServices);
  });
});

// ---- filter, "Mehr anzeigen", "Aktualisieren" ------------------------------------------

describe('activity list tools', () => {
  it('filters by name and description, and by state, and says when nothing matches', async () => {
    await open(failedList());
    const q = byTestId('act-query') as HTMLInputElement;
    expect(screen.getByLabelText(t.actFilterLabel)).toBe(q);
    await fireEvent.input(q, { target: { value: 'NIGHTLY' } });
    expect(screen.getAllByTestId('act-row').map((r) => r.dataset.name)).toEqual(['demo-batch.service']);
    await fireEvent.input(q, { target: { value: 'ssh.ser' } });
    expect(screen.getAllByTestId('act-row').map((r) => r.dataset.name)).toEqual(['ssh.service']);
    expect(text(byTestId('act-shown'))).toBe(fmt('actShown', { shown: 1, total: 1 }));
    await fireEvent.input(q, { target: { value: '' } });
    await fireEvent.click(screen.getByLabelText(t['actFilter.failed']));
    expect(screen.getAllByTestId('act-row').map((r) => r.dataset.name)).toEqual(['demo-broken.service', 'example-crash.service', 'sample-sync.service']);
    await fireEvent.click(screen.getByLabelText(t['actFilter.inactive']));
    expect(screen.getAllByTestId('act-row').every((r) => text(within(r).getByTestId('act-state')) === 'inaktiv')).toBe(true);
    await fireEvent.click(screen.getByLabelText(t['actFilter.active']));
    expect(screen.getAllByTestId('act-row').every((r) => text(within(r).getByTestId('act-state')) === 'aktiv')).toBe(true);
    await fireEvent.input(q, { target: { value: 'kein-solcher-dienst' } });
    expect(text(byTestId('act-no-match'))).toBe(t.actNoMatch);
    await fireEvent.click(screen.getByLabelText(t['actFilter.all']));
    await fireEvent.input(q, { target: { value: '' } });
    expect(screen.getAllByTestId('act-row').length).toBe(failedList().services.length);
    // The state filter is a labelled group of native radio inputs.
    expect(screen.getByRole('group', { name: t.actStateFilterLabel })).toBeTruthy();
    expect(screen.getAllByRole('radio').length).toBe(5);
  });

  it('the empty filter state of the scenario list-filtered-empty', async () => {
    await open(noFailedList());
    await fireEvent.click(screen.getByLabelText(t['actFilter.failed']));
    expect(text(byTestId('act-no-match'))).toBe(t.actNoMatch);
  });

  it('renders at most 100 rows, then "Mehr anzeigen" adds the next 100; a new filter starts again at 100', async () => {
    await open(longList(250));
    expect(screen.getAllByTestId('act-row').length).toBe(100);
    expect(text(byTestId('act-shown'))).toBe(fmt('actShown', { shown: 100, total: 250 }));
    await fireEvent.click(byTestId('act-more'));
    expect(screen.getAllByTestId('act-row').length).toBe(200);
    await fireEvent.click(byTestId('act-more'));
    expect(screen.getAllByTestId('act-row').length).toBe(250);
    expect(screen.queryByTestId('act-more')).toBeNull();
    await fireEvent.input(byTestId('act-query'), { target: { value: 'demo-unit' } });
    expect(screen.getAllByTestId('act-row').length).toBe(100);
  });

  it('"Aktualisieren" reads once and shows the new reading; no automatic refresh', async () => {
    await open(sampleList());
    expect(count(LIST)).toBe(1);
    await flush(60_000);
    expect(count(LIST)).toBe(1);
    expect(count(LAST)).toBe(1);
    routes[LIST] = () => ok({ ...sampleList(), read_at: '2026-11-25T10:05:00Z' });
    await fireEvent.click(byTestId('act-refresh'));
    await flush();
    expect(count(LIST)).toBe(2);
  });

  it('a failed refresh keeps the list and says so in the message area', async () => {
    let fail = false;
    await open(() => (fail ? 'network' : ok(sampleList())));
    fail = true;
    await fireEvent.click(byTestId('act-refresh'));
    await flush();
    expect(text(byTestId('act-load-message'))).toBe(t.actLoadError);
    expect(byTestId('act-load-message').getAttribute('role')).toBe('alert');
    expect(screen.getAllByTestId('act-row').length).toBe(sampleList().services.length);
  });
});

// ---- the panel -------------------------------------------------------------------------------

describe('activity action panel', () => {
  it('offers the buttons the row allows, each with its effect text, and the critical note', async () => {
    await open(sampleList());
    const buttonsOf = async (name: string) => {
      await fireEvent.click(within(rowOf(name)).getByTestId('act-actions'));
      await flush();
      const panel = byTestId('act-panel');
      const labels = within(panel).getAllByRole('button').map((b) => b.textContent?.trim());
      return { panel, labels };
    };
    let r = await buttonsOf('demo-app.service');
    expect(r.labels).toEqual([t.actStop, t.actRestart, t.actClose]);
    expect(text(r.panel)).toContain(t['actEffect.stop']);
    expect(text(r.panel)).toContain(t['actEffect.restart']);
    expect(within(r.panel).queryByTestId('act-critical-note')).toBeNull();
    r = await buttonsOf('demo-batch.service');
    expect(r.labels).toEqual([t.actStart, t.actRestart, t.actClose]);
    expect(text(r.panel)).toContain(t['actEffect.start']);
    r = await buttonsOf('ssh.service');
    expect(r.labels).toEqual([t.actRestart, t.actClose]);
    expect(text(within(r.panel).getByTestId('act-critical-note'))).toBe(t['actCritical.ssh']);
    r = await buttonsOf('docker.service');
    expect(r.labels).toEqual([t.actStop, t.actRestart, t.actClose]);
    expect(text(within(r.panel).getByTestId('act-critical-note'))).toBe(t['actCritical.container']);
    r = await buttonsOf('tailscaled.service');
    expect(text(within(r.panel).getByTestId('act-critical-note'))).toBe(t['actCritical.remote_access']);
    r = await buttonsOf('chrony.service');
    expect(text(within(r.panel).getByTestId('act-critical-note'))).toBe(t['actCritical.time']);
    r = await buttonsOf('NetworkManager.service');
    expect(text(within(r.panel).getByTestId('act-critical-note'))).toBe(t['actCritical.network']);
    // A one-shot unit is not known on the read path: it gets buttons, and the broker refuses (A3.3).
  });

  it('moves the focus into the panel and back; Escape and "Schließen" close it; one panel at a time', async () => {
    await open(sampleList());
    const btnA = within(rowOf('demo-app.service')).getByTestId('act-actions');
    const btnB = within(rowOf('demo-batch.service')).getByTestId('act-actions');
    expect(btnA.getAttribute('aria-expanded')).toBe('false');
    expect(btnA.getAttribute('aria-label')).toBe('Aktionen für demo-app.service');
    expect(btnA.textContent?.trim()).toBe(t.actActions);
    await fireEvent.click(btnA);
    await flush();
    const panel = byTestId('act-panel');
    expect(document.activeElement).toBe(panel);
    expect(btnA.getAttribute('aria-expanded')).toBe('true');
    expect(btnA.getAttribute('aria-controls')).toBe(panel.id);
    expect(screen.getByRole('region', { name: 'Aktionen für demo-app.service' })).toBe(panel);
    expect(panel.querySelector('h3')).toBeTruthy();
    await fireEvent.keyDown(window, { key: 'Escape' });
    await flush();
    expect(screen.queryByTestId('act-panel')).toBeNull();
    expect(document.activeElement).toBe(btnA);
    // One at a time.
    await fireEvent.click(btnA);
    await flush();
    await fireEvent.click(btnB);
    await flush();
    expect(screen.getAllByTestId('act-panel').length).toBe(1);
    expect(within(rowOf('demo-batch.service')).getByTestId('act-panel')).toBeTruthy();
    await fireEvent.click(byTestId('act-close'));
    await flush();
    expect(screen.queryByTestId('act-panel')).toBeNull();
    expect(document.activeElement).toBe(btnB);
    // The same button closes it again.
    await fireEvent.click(btnB);
    await flush();
    await fireEvent.click(btnB);
    await flush();
    expect(screen.queryByTestId('act-panel')).toBeNull();
  });

  it('uses native elements only: buttons, a labelled search field and radio inputs', async () => {
    await open(sampleList());
    const page = byTestId('activity');
    // Changed on purpose in G7.5: the services card has no modal; the one native
    // dialog of the page is the restart and shutdown confirmation (ActivityPower.test.ts).
    const services = page.querySelector('[aria-labelledby="services-heading"]')!;
    expect(services.querySelector('[role="button"], [tabindex]:not([tabindex="-1"]), select, dialog, [aria-modal]')).toBeNull();
    for (const b of services.querySelectorAll('button')) expect(b.getAttribute('type')).toBe('button');
    expect(page.querySelector('[role="button"], [tabindex]:not([tabindex="-1"]), select, [aria-modal]')).toBeNull();
    expect([...page.querySelectorAll('dialog')].map((d) => d.getAttribute('data-testid'))).toEqual(['pwr-dialog']);
  });
});

// ---- the action flow ----------------------------------------------------------------------------

describe('activity action flow', () => {
  async function startRestart(after: ActionState[]): Promise<void> {
    const queue = [...after];
    await open(sampleList(), {
      [POST]: () => ok({ schema_version: 1, accepted: true }, 202),
      [LAST]: () => ok(count(POST) === 0 ? noAction : queue.length > 1 ? queue.shift()! : queue[0]!),
    });
    await fireEvent.click(within(rowOf('demo-app.service')).getByTestId('act-actions'));
    await flush();
    await fireEvent.click(byTestId('act-do-restart'));
    await flush();
  }

  it('confirms with one click, sends unit and action, polls every 2 s while it runs, says the result, reads the list once', async () => {
    await startRestart([running('demo-app.service', 'restart'), running('demo-app.service', 'restart'), done('demo-app.service', 'restart', 'success', { state: 'active' })]);
    expect(calls.filter((c) => c.key === POST).map((c) => c.body)).toEqual([{ unit: 'demo-app.service', action: 'restart' }]);
    // The panel closed and the focus is back on the row's button.
    expect(screen.queryByTestId('act-panel')).toBeNull();
    expect(document.activeElement).toBe(within(rowOf('demo-app.service')).getByTestId('act-actions'));
    expect(live()).toBe('Neustart für demo-app.service läuft …');
    expect(byTestId('act-live').getAttribute('role')).toBe('status');
    // While it runs every action button is locked.
    await fireEvent.click(within(rowOf('demo-batch.service')).getByTestId('act-actions'));
    await flush();
    expect((byTestId('act-do-start') as HTMLButtonElement).disabled).toBe(true);
    const lastBefore = count(LAST);
    await flush(1999);
    expect(count(LAST)).toBe(lastBefore);
    await flush(1);
    expect(count(LAST)).toBe(lastBefore + 1);
    await flush(2000);
    await flush(2000);
    expect(live()).toBe(`Neustart für demo-app.service. ${t.actResultSuccess.replace('{state}', 'aktiv')}`);
    expect(live()).toContain('Ein späterer Ausfall wird nicht geprüft.');
    expect(count(LIST)).toBe(2);
    // Polling has stopped.
    const after = count(LAST);
    await flush(20_000);
    expect(count(LAST)).toBe(after);
    expect(count(LIST)).toBe(2);
  });

  it('says every result code as its own sentence, never the server text', async () => {
    for (const code of serviceCodes.filter((c) => c !== 'invalid_request')) {
      await startRestart([done('demo-app.service', 'restart', code, code === 'rate_limited' ? { retry_after_s: 37 } : { state: 'failed' })]);
      await flush(2000);
      const want = resultText(code, 'failed', code === 'rate_limited' ? 37 : undefined);
      expect(live(), code).toBe(`Neustart für demo-app.service. ${want}`);
      expect(live()).not.toContain('English server text');
      expect(want).not.toBe(t.errorGeneric);
      cleanup();
    }
    expect(resultText('timeout', undefined, undefined)).toBe(t['svcErr.timeout']);
    expect(resultText('unit_oneshot', undefined, undefined)).toBe('Dieser Dienst führt eine einmalige Aufgabe aus. Oarfish verwaltet solche Aufgaben nicht.');
    expect(resultText('rate_limited', undefined, 37)).toBe('Zu viele Aktionen in kurzer Zeit. Eine neue Aktion ist in 37 Sekunden möglich.');
    expect(resultText('job_failed', 'failed', undefined)).toBe('Die Aktion ist fehlgeschlagen. Zustand jetzt: fehlgeschlagen.');
    expect(resultText('state_unexpected', 'zombie', undefined)).toContain('Zustand jetzt: Unbekannt.');
    expect(resultText('something_new', undefined, undefined)).toBe(t.errorGeneric);
  });

  it('answers of the action route itself: busy, unavailable (the list is read again), invalid, network', async () => {
    for (const [reply, want, reread] of [
      [err(409, 'busy'), t['svcErr.busy'], false],
      [err(503, 'unavailable'), t['svcErr.unavailable'], true],
      [err(400, 'invalid_request'), t['svcErr.invalid_request'], true],
      [err(403, 'csrf_failed'), t.errorGeneric, true],
      ['network', t.errorNetwork, false],
    ] as const) {
      await open(sampleList(), { [POST]: () => reply });
      await fireEvent.click(within(rowOf('demo-app.service')).getByTestId('act-actions'));
      await flush();
      await fireEvent.click(byTestId('act-do-stop'));
      await flush();
      expect(live()).toBe(`Stop für demo-app.service. ${want}`);
      expect(count(LIST)).toBe(reread ? 2 : 1);
      await flush(10_000);
      expect(count(LAST)).toBe(1); // only the read on opening: nothing runs, so nothing polls
      cleanup();
    }
    expect(errorText('busy')).toBe(t['svcErr.busy']);
  });

  it('the component cleanup stops the polling when the page is left', async () => {
    await startRestart([running('demo-app.service', 'restart')]);
    await flush(4000);
    const n = count(LAST);
    cleanup();
    await flush(20_000);
    expect(count(LAST)).toBe(n);
  });

  it('follows an action that runs already when the page opens', async () => {
    const queue = [running('demo-cache.service', 'stop'), running('demo-cache.service', 'stop'), done('demo-cache.service', 'stop', 'success', { state: 'inactive' })];
    await open(sampleList(), { [LAST]: () => ok(queue.length > 1 ? queue.shift()! : queue[0]!) });
    expect(live()).toBe('Stop für demo-cache.service läuft …');
    await flush(2000);
    await flush(2000);
    expect(live()).toBe(`Stop für demo-cache.service. ${t.actResultSuccess.replace('{state}', 'inaktiv')}`);
  });

  it('a 401 takes the existing session-loss path', async () => {
    const lost = vi.fn();
    const off = onSessionLost(lost);
    await open(() => err(401, 'unauthenticated'));
    expect(lost).toHaveBeenCalledWith('expired');
    expect(screen.queryByRole('alert')).toBeNull();
    off();
  });

  it('English: the same page in the other language', async () => {
    setLang('en');
    await startRestart([done('demo-app.service', 'restart', 'success', { state: 'active' })]);
    expect(screen.getByRole('heading', { level: 1, name: en.activityTitle })).toBeTruthy();
    expect(text(within(rowOf('demo-app.service')).getByTestId('act-mode'))).toBe('Start mode: Automatic');
    await flush(2000);
    expect(live()).toBe('Restart for demo-app.service. The action was carried out. State now: active. A later failure is not checked.');
    expect(text(within(rowOf('ssh.service')).getByTestId('act-restart-only'))).toBe(`${en.actRestartOnlyRow}. ${en.actRestartOnlyNote}`);
  });
});

// ---- the development scenario mode -----------------------------------------------------------

describe('activity scenario mode', () => {
  it('every scenario of docs/UI.md exists', () => {
    expect(servicesScenarioNames()).toEqual([
      'loading',
      'load-error',
      'systemd-unavailable',
      'list',
      'list-long',
      'list-filtered-empty',
      'list-with-failed',
      'list-in-transition',
      'broker-unavailable',
      'action-flow',
      'action-timeout',
      'action-rate-limited',
      'action-oneshot-refused',
      'action-restart-only-note',
      'action-denied-row',
      'session-lost',
      // G7.5, tested in ActivityPower.test.ts
      'power-dialog-reboot',
      'power-dialog-shutdown',
      'power-waiting-list',
      'power-wrong-password',
      'power-rate-limited',
      'power-update-running',
      'power-already-requested',
      'power-broker-unavailable',
      'power-reboot-flow',
      'power-reboot-too-long',
      'power-shutdown-flow',
      'power-session-lost',
    ]);
    expect(installServicesMock('not-a-scenario')).toBe(false);
  });

  for (const name of servicesScenarioNames()) {
    it(`scenario ${name} renders its state`, async () => {
      vi.stubGlobal('fetch', vi.fn(async () => new Response('{}', { status: 404 })));
      const lost = vi.fn();
      const off = onSessionLost(lost);
      expect(installServicesMock(name)).toBe(true);
      render(Activity);
      await flush(500);
      const page = byTestId('activity');
      switch (name) {
        case 'loading':
          expect(text(screen.getByRole('status'))).toContain(t.actLoading);
          break;
        case 'load-error':
          expect(text(screen.getByRole('alert'))).toContain(t.actLoadError);
          break;
        case 'systemd-unavailable':
          expect(text(page)).toContain(t.actSystemdMissing);
          break;
        case 'session-lost':
          expect(lost).toHaveBeenCalled();
          break;
        case 'broker-unavailable':
        case 'power-broker-unavailable':
          expect(byTestId('act-broker-missing')).toBeTruthy();
          break;
        case 'list-long':
          expect(screen.getAllByTestId('act-row').length).toBe(100);
          break;
        case 'list-in-transition':
          expect(text(byTestId('act-counts'))).toContain('1 im Übergang');
          break;
        default: {
          expect(screen.getAllByTestId('act-row').length).toBeGreaterThan(5);
          if (name.startsWith('action-') && !name.endsWith('-note') && !name.endsWith('-row')) {
            await fireEvent.click(within(rowOf('demo-app.service')).getByTestId('act-actions'));
            await flush();
            await fireEvent.click(byTestId('act-do-restart'));
            await flush(500);
            expect(live()).toContain('läuft …');
            await flush(12_000);
            const want = { 'action-flow': 'Zustand jetzt: aktiv', 'action-timeout': t['svcErr.timeout'], 'action-rate-limited': '37 Sekunden', 'action-oneshot-refused': t['svcErr.unit_oneshot'] }[name]!;
            expect(live()).toContain(want);
          }
        }
      }
      off();
    });
  }
});

// Both languages have every text of the page (the i18n test checks the keys; this checks the wording rules of the prompt).
describe('activity texts', () => {
  it('the success sentence says that a later failure is not verified, in both languages', () => {
    expect(de.actResultSuccess).toContain('Ein späterer Ausfall wird nicht geprüft.');
    expect(en.actResultSuccess).toContain('A later failure is not checked.');
  });
});
