// The client of the services routes (G7.4): run-time shape checks, the result
// kinds, and the sentences of every code in both languages.
import { afterEach, describe, expect, it, vi } from 'vitest';
import { de, en } from './i18n';
import { setLang } from './lang.svelte';
import { forgetSession, onSessionLost } from './session';
import { done, hostileList, longList, noAction, running, sampleList } from './services.fixtures';
import { activeStates, criticalKinds, fetchAction, fetchServices, isActionState, isServicesList, startModes, submitAction } from './services';
import { criticalNote, errorText, kindText, resultText, serviceCodes, startModeText, stateText } from './serviceErrors';

const reply = (status: number, body: unknown) =>
  ({ ok: status >= 200 && status < 300, status, headers: new Headers(), json: async () => structuredClone(body) }) as unknown as Response;
const fetchOf = (r: Response | Error) => vi.fn(async () => (r instanceof Error ? Promise.reject(r) : r)) as unknown as typeof fetch;

afterEach(() => {
  forgetSession();
  setLang('de');
});

describe('shape checks', () => {
  it('accepts the fixtures, including unknown words of the closed sets', () => {
    for (const l of [sampleList(), longList(10), hostileList()]) expect(isServicesList(l)).toBe(true);
    for (const a of [noAction, running('a.service', 'stop'), done('a.service', 'stop', 'success', { state: 'inactive' }), done('a.service', 'restart', 'something_new')]) {
      expect(isActionState(a)).toBe(true);
    }
  });

  it('refuses another schema, a missing or mistyped member, and a step outside the closed set', () => {
    const l = sampleList();
    const broken: unknown[] = [
      null,
      [],
      { ...l, schema_version: 2 },
      { ...l, read_at: 5 },
      { ...l, broker_available: 'yes' },
      { ...l, counts: { ...l.counts, failed: -1 } },
      { ...l, counts: { ...l.counts, total: 1.5 } },
      { ...l, services: null },
      { ...l, services: [{ ...l.services[0], name: '' }] },
      { ...l, services: [{ ...l.services[0], actions: 'stop' }] },
      { ...l, services: [{ ...l.services[0], denied: 1 }] },
      { ...l, services: [{ ...l.services[0], critical: null }] },
    ];
    for (const b of broken) expect(isServicesList(b)).toBe(false);
    for (const b of [
      { schema_version: 1 },
      { schema_version: 2, action: null },
      { schema_version: 1, action: { ...running('a.service', 'stop').action, step: 'queued' } },
      { schema_version: 1, action: { ...done('a.service', 'stop', 'x').action, retry_after_s: -1 } },
      { schema_version: 1, action: { ...done('a.service', 'stop', 'x').action, code: 7 } },
    ]) {
      expect(isActionState(b)).toBe(false);
    }
  });
});

describe('requests', () => {
  it('reads the list, tells systemd missing from other failures, and takes the session-loss path on 401', async () => {
    expect(await fetchServices(fetchOf(reply(200, sampleList())))).toEqual({ ok: true, list: sampleList() });
    expect(await fetchServices(fetchOf(reply(200, { schema_version: 1 })))).toEqual({ ok: false, kind: 'shape' });
    expect(await fetchServices(fetchOf(reply(503, { error: { code: 'services_unavailable' } })))).toEqual({ ok: false, kind: 'systemd' });
    expect(await fetchServices(fetchOf(reply(503, { error: { code: 'services_read_failed' } })))).toEqual({ ok: false, kind: 'network' });
    expect(await fetchServices(fetchOf(new TypeError('offline')))).toEqual({ ok: false, kind: 'network' });
    const lost = vi.fn();
    const off = onSessionLost(lost);
    expect(await fetchServices(fetchOf(reply(401, { error: { code: 'unauthenticated' } })))).toEqual({ ok: false, kind: 'session' });
    expect(lost).toHaveBeenCalledWith('expired');
    off();
    expect(await fetchAction(fetchOf(reply(200, noAction)))).toEqual({ ok: true, state: noAction });
    expect(await fetchAction(fetchOf(reply(200, { schema_version: 1, action: 'x' })))).toEqual({ ok: false, kind: 'shape' });
  });

  it('sends the action as JSON with unit and action only, and reads 202', async () => {
    const f = vi.fn(async () => reply(202, { schema_version: 1, accepted: true })) as unknown as typeof fetch;
    expect(await submitAction('demo-app.service', 'restart', f)).toEqual({ ok: true });
    const [url, init] = (f as unknown as { mock: { calls: [string, RequestInit][] } }).mock.calls[0]!;
    expect(url).toBe('/api/v1/services/action');
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body as string)).toEqual({ unit: 'demo-app.service', action: 'restart' });
    expect((init.headers as Record<string, string>)['Content-Type']).toBe('application/json');
    expect(await submitAction('a.service', 'stop', fetchOf(reply(200, { schema_version: 1, accepted: true })))).toEqual({ ok: false, kind: 'shape' });
    expect(await submitAction('a.service', 'stop', fetchOf(reply(409, { error: { code: 'busy' } })))).toEqual({ ok: false, kind: 'error', code: 'busy', retryAfter: null });
    expect(await submitAction('a.service', 'stop', fetchOf(new TypeError('x')))).toEqual({ ok: false, kind: 'network' });
  });
});

describe('sentences', () => {
  it('every code has its own sentence in both languages, and nothing else gets one', () => {
    for (const lang of ['de', 'en'] as const) {
      setLang(lang);
      const dict = lang === 'de' ? de : en;
      for (const code of serviceCodes) {
        const s = errorText(code, 'failed', 12);
        expect(s, code).not.toBe(dict.errorGeneric);
        expect(s, code).not.toMatch(/\{\w+\}/);
      }
      expect(errorText('accepted')).toBe(dict.errorGeneric);
      expect(errorText('update_running')).toBe(dict.errorGeneric);
      expect(errorText('constructor')).toBe(dict.errorGeneric);
    }
  });

  it('the state words, the start modes and the kinds come from their closed sets; anything else is neutral', () => {
    expect(activeStates.map((s) => stateText(s))).toEqual(['aktiv', 'lädt neu', 'inaktiv', 'fehlgeschlagen', 'startet', 'stoppt', 'Wartung', 'aktualisiert']);
    expect(startModes.map((m) => startModeText(m))).toEqual([
      'Automatisch',
      'Automatisch bis zum Neustart',
      'Manuell',
      'Nur bei Bedarf',
      'Gesperrt',
      'Indirekt',
      'Verknüpft',
      'Erzeugt',
      'Vorübergehend',
    ]);
    expect(stateText('zombie')).toBe('Unbekannt');
    expect(stateText(undefined)).toBe('Unbekannt');
    expect(startModeText('')).toBe('Unbekannt');
    expect(startModeText('alias')).toBe('Unbekannt');
    for (const k of criticalKinds) expect(criticalNote(k)).toBeTruthy();
    expect(criticalNote('quantum')).toBeNull();
    expect(kindText('quantum')).toBe('Sonstiges');
  });

  it('the success sentence names the state and says that a later failure is not checked', () => {
    expect(resultText('success', 'active', undefined)).toBe('Die Aktion wurde ausgeführt. Zustand jetzt: aktiv. Ein späterer Ausfall wird nicht geprüft.');
    setLang('en');
    expect(resultText('success', 'inactive', undefined)).toBe('The action was carried out. State now: inactive. A later failure is not checked.');
  });
});
