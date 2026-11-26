// The client of the power routes (G7.5): run-time shape checks of every
// answer, the request it sends, the session-loss path, the /healthz probe, the
// sentences of every code and of every waiting kind, in both languages.
import { afterEach, describe, expect, it, vi } from 'vitest';
import { fetchPower, isPowerState, onPowerAccepted, powerAccepted, powerRequestTimeoutMs, probeHealth, requestPower } from './power';
import { powerCodes, powerErrorText, waitingText } from './powerErrors';
import { everyWaitingKind, powerState, singleCounts } from './power.fixtures';
import { de, en, fmt, t } from './i18n';
import { setLang } from './lang.svelte';
import { forgetSession, onSessionLost } from './session';

const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
const apiError = (status: number, code: string, extra: Record<string, unknown> = {}) =>
  json({ error: { code, message: 'English server text that must not be shown', recovery: 'x', correlation_id: '0123456789abcdef', ...extra } }, status);

afterEach(() => {
  vi.unstubAllGlobals();
  forgetSession();
  setLang('de');
});

describe('power state shape', () => {
  it('accepts schema 1 with the broker flag and a list of items, each with a kind and an optional count of at least 1', () => {
    expect(isPowerState(powerState())).toBe(true);
    expect(isPowerState(powerState(everyWaitingKind(), false))).toBe(true);
    expect(isPowerState(powerState(singleCounts()))).toBe(true);
  });

  it('refuses everything else', () => {
    for (const v of [
      null,
      [],
      {},
      { schema_version: 2, broker_available: true, waiting: [] },
      { schema_version: 1, broker_available: 'yes', waiting: [] },
      { schema_version: 1, broker_available: true },
      { schema_version: 1, broker_available: true, waiting: {} },
      { schema_version: 1, broker_available: true, waiting: [{}] },
      { schema_version: 1, broker_available: true, waiting: [{ kind: '' }] },
      { schema_version: 1, broker_available: true, waiting: [{ kind: 7 }] },
      { schema_version: 1, broker_available: true, waiting: [{ kind: 'other_sessions', count: 0 }] },
      { schema_version: 1, broker_available: true, waiting: [{ kind: 'other_sessions', count: 1.5 }] },
      { schema_version: 1, broker_available: true, waiting: [{ kind: 'other_sessions', count: '2' }] },
      { schema_version: 1, broker_available: true, waiting: ['update_staged'] },
    ]) {
      expect(isPowerState(v), JSON.stringify(v)).toBe(false);
    }
  });
});

describe('power requests', () => {
  it('GET /api/v1/power: the state, a 401 takes the session-loss path, power_unavailable, no answer, a wrong shape', async () => {
    const lost = vi.fn();
    const off = onSessionLost(lost);
    const f = vi.fn(async () => json(powerState([{ kind: 'update_staged' }])));
    expect(await fetchPower(f as typeof fetch)).toEqual({ ok: true, state: powerState([{ kind: 'update_staged' }]) });
    const [url, init] = f.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe('/api/v1/power');
    expect(init.method).toBeUndefined();
    expect(init.cache).toBe('no-store');
    expect(await fetchPower((async () => apiError(401, 'unauthenticated')) as typeof fetch)).toEqual({ ok: false, kind: 'session' });
    expect(lost).toHaveBeenCalledWith('expired');
    expect(await fetchPower((async () => apiError(503, 'power_unavailable')) as typeof fetch)).toEqual({ ok: false, kind: 'unavailable' });
    expect(await fetchPower((async () => apiError(500, 'x')) as typeof fetch)).toEqual({ ok: false, kind: 'network' });
    expect(await fetchPower((async () => Promise.reject(new TypeError('offline'))) as typeof fetch)).toEqual({ ok: false, kind: 'network' });
    expect(await fetchPower((async () => json({ schema_version: 1 })) as typeof fetch)).toEqual({ ok: false, kind: 'shape' });
    expect(await fetchPower((async () => new Response('not json')) as typeof fetch)).toEqual({ ok: false, kind: 'shape' });
    off();
  });

  it('POST /api/v1/power sends the action and the password once, with a timeout above the server worst case', async () => {
    const timeout = vi.spyOn(AbortSignal, 'timeout');
    const f = vi.fn(async () => json({ schema_version: 1, accepted: true }));
    expect(await requestPower('poweroff', 'Fake-Pw-For-Tests-1', f as typeof fetch)).toEqual({ ok: true });
    expect(f).toHaveBeenCalledTimes(1);
    const [url, init] = f.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe('/api/v1/power');
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body as string)).toEqual({ action: 'poweroff', current_password: 'Fake-Pw-For-Tests-1' });
    expect(timeout).toHaveBeenCalledWith(powerRequestTimeoutMs);
    expect(powerRequestTimeoutMs).toBeGreaterThan(23500); // 0.5 s hash + 3 s update read + 20 s broker
    expect(powerRequestTimeoutMs).toBeLessThan(30000); // inside the server's write timeout
    timeout.mockRestore();
  });

  it('POST /api/v1/power: accepted needs 200 and the exact body; errors keep their code and wait time', async () => {
    expect(await requestPower('reboot', 'x', (async () => json({ schema_version: 1, accepted: true }, 202)) as typeof fetch)).toEqual({ ok: false, kind: 'shape' });
    expect(await requestPower('reboot', 'x', (async () => json({ schema_version: 1, accepted: false })) as typeof fetch)).toEqual({ ok: false, kind: 'shape' });
    expect(await requestPower('reboot', 'x', (async () => json({ accepted: true })) as typeof fetch)).toEqual({ ok: false, kind: 'shape' });
    expect(await requestPower('reboot', 'x', (async () => apiError(429, 'rate_limited', { retry_after_seconds: 37 })) as typeof fetch)).toEqual({
      ok: false,
      kind: 'error',
      code: 'rate_limited',
      retryAfter: 37,
    });
    expect(await requestPower('reboot', 'x', (async () => apiError(409, 'update_running')) as typeof fetch)).toEqual({ ok: false, kind: 'error', code: 'update_running', retryAfter: null });
    expect(await requestPower('reboot', 'x', (async () => apiError(503, 'power_unavailable')) as typeof fetch)).toEqual({ ok: false, kind: 'unavailable' });
    expect(await requestPower('reboot', 'x', (async () => Promise.reject(new TypeError('offline'))) as typeof fetch)).toEqual({ ok: false, kind: 'network' });
    const lost = vi.fn();
    const off = onSessionLost(lost);
    expect(await requestPower('reboot', 'x', (async () => apiError(401, 'unauthenticated')) as typeof fetch)).toEqual({ ok: false, kind: 'session' });
    expect(lost).toHaveBeenCalledWith('expired');
    off();
  });

  it('GET /healthz: true only for an answer in the 2xx range; no session cookie is sent', async () => {
    const f = vi.fn(async () => json({ status: 'ok' }));
    expect(await probeHealth(f as typeof fetch)).toBe(true);
    const [url, init] = f.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe('/healthz');
    expect(init.credentials).toBe('omit');
    expect(await probeHealth((async () => new Response('', { status: 502 })) as typeof fetch)).toBe(false);
    expect(await probeHealth((async () => Promise.reject(new TypeError('offline'))) as typeof fetch)).toBe(false);
  });

  it('the accepted signal reaches every listener once and stops when told', () => {
    const a = vi.fn();
    const b = vi.fn();
    const offA = onPowerAccepted(a);
    const offB = onPowerAccepted(b);
    powerAccepted('reboot');
    offA();
    powerAccepted('poweroff');
    offB();
    expect(a.mock.calls).toEqual([['reboot']]);
    expect(b.mock.calls).toEqual([['reboot'], ['poweroff']]);
  });
});

describe('power sentences', () => {
  it('every code of a refused request has its own sentence, never the server text', () => {
    const texts = new Set<string>();
    for (const code of powerCodes) {
      const r = powerErrorText({ ok: false, kind: 'error', code, retryAfter: null });
      expect(r.text, code).toBe(t[`pwrErr.${code}`]);
      expect(r.field).toBeNull();
      texts.add(r.text);
    }
    expect(texts.size).toBe(powerCodes.length);
    expect(powerErrorText({ ok: false, kind: 'error', code: 'wrong_password', retryAfter: null })).toEqual({ text: t['pwrErr.wrong_password'], field: 'password' });
    // Its own sentence, not the account page's "current password" (G7-FIX1).
    expect(de['pwrErr.wrong_password']).toBe('Das Passwort ist falsch.');
    expect(en['pwrErr.wrong_password']).toBe('The password is wrong.');
    expect(de.errorCurrentPassword).toBe('Das aktuelle Passwort ist falsch.');
    expect(powerErrorText({ ok: false, kind: 'error', code: 'rate_limited', retryAfter: 37 }).text).toBe(fmt('errorRateLimitedSeconds', { seconds: 37 }));
    expect(powerErrorText({ ok: false, kind: 'error', code: 'csrf_failed', retryAfter: null }).text).toBe(t['updErr.request_rejected']);
    expect(powerErrorText({ ok: false, kind: 'error', code: 'invalid_request', retryAfter: null }).text).toBe(t['updErr.bad_request']);
    expect(powerErrorText({ ok: false, kind: 'error', code: 'something_new', retryAfter: null }).text).toBe(t.errorGeneric);
    expect(powerErrorText({ ok: false, kind: 'network' }).text).toBe(t.errorNetwork);
    expect(powerErrorText({ ok: false, kind: 'unavailable' }).text).toBe(t['pwrErr.unavailable']);
    expect(powerErrorText({ ok: false, kind: 'shape' }).text).toBe(t.errorGeneric);
  });

  it('the sentences of B2 for update_running and already_requested, in both languages', () => {
    expect(de['pwrErr.update_running']).toBe('Gerade läuft ein Oarfish-Update. Neustart und Herunterfahren sind erst danach möglich.');
    expect(en['pwrErr.update_running']).toBe('An Oarfish update is running. Restart and shutdown are possible only after it.');
    expect(de['pwrErr.already_requested']).toBe('Neustart oder Herunterfahren wurde bereits angefordert.');
    expect(en['pwrErr.already_requested']).toBe('A restart or shutdown was already requested.');
  });

  it('every waiting kind in words, with one and with many; an unknown kind neutrally', () => {
    const got = everyWaitingKind().map(waitingText);
    expect(got).toEqual([
      t['pwrWait.update_staged'],
      t['pwrWait.update_running'],
      t['pwrWait.service_action_running'],
      fmt('pwrWait.other_sessions.many', { count: 2 }),
      fmt('pwrWait.other_views.many', { count: 3 }),
      t['pwrWait.other'],
    ]);
    expect(singleCounts().map(waitingText)).toEqual([t['pwrWait.other_sessions.one'], t['pwrWait.other_views.one']]);
    expect(got[3]).toBe('2 weitere Oarfish-Sitzungen enden.');
    setLang('en');
    expect(waitingText({ kind: 'other_views', count: 3 })).toBe('3 open live views lose their connection.');
    expect(waitingText({ kind: 'update_staged' })).toBe('A prepared update will be discarded.');
  });

  it('the B6 texts, with "Bitte" left out (UI.md text rules)', () => {
    expect(de.pwrAfterReboot).toBe('Das Gerät ist wieder erreichbar. Melden Sie sich an.');
    expect(en.pwrAfterReboot).toBe('The device can be reached again. Sign in.');
    expect(de.pwrRebootTitle).toBe('Gerät neu starten?');
    expect(de.pwrPoweroffBody).toContain('trennen Sie die Stromversorgung und schließen Sie sie wieder an');
    expect(de.pwrTunnel).toBe('Wenn Sie über einen SSH-Tunnel verbunden sind, wird er getrennt. Bauen Sie ihn danach neu auf.');
    expect(de.pwrTunnelBack).toBe('Wenn Sie über einen SSH-Tunnel verbunden waren, bauen Sie ihn jetzt neu auf. Dann melden Sie sich an.');
    expect(en.pwrTunnelBack).toBe('If you were connected through an SSH tunnel, set it up again now. Then sign in.');
    expect(de.pwrWaitingNone).toBe('Keine wartenden Aktionen');
  });
});
