// The update client (lib/update.ts) and the error sentences (lib/updateErrors.ts).
import { afterEach, describe, expect, it, vi } from 'vitest';
import { de, en, t } from './i18n';
import { fetchSession, forgetSession, onSessionLost, postJSON } from './session';
import { sentinelCsrf, signedIn } from './session.fixtures';
import { applyUpdate, checkForUpdate, discardUpdate, fetchUpdateState, isUpdateState, jobSteps } from './update';
import { available, updateScenarios, upToDate, withState } from './update.fixtures';
import { actionErrorText, durationText, updateCodes, updateErrorText } from './updateErrors';

const json = (status: number, body: unknown) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });

afterEach(() => {
  forgetSession();
  vi.unstubAllGlobals();
});

describe('State shape check', () => {
  it('accepts every fixture state', () => {
    for (const [name, s] of Object.entries(updateScenarios())) expect(isUpdateState(s), name).toBe(true);
  });

  it('refuses another schema version, a missing member, a wrong type and a step outside the closed set', () => {
    const base = available();
    const bad: unknown[] = [
      null,
      [],
      'state',
      { ...base, schema_version: 2 },
      withState(base, (s) => delete (s as Partial<typeof s>).helper),
      withState(base, (s) => ((s.check as { state: string }).state = 'maybe')),
      withState(base, (s) => ((s.available as { download_size: unknown }).download_size = '3 MiB')),
      withState(base, (s) => ((s.available as { download_size: unknown }).download_size = -1)),
      withState(base, (s) => ((s.available as { channel: string }).channel = 'nightly')),
      withState(base, (s) => (s.job = { kind: 'apply', step: 'rebooting' as 'done', running: true, started_at: 'x' })),
      withState(base, (s) => (s.job = { kind: 'install' as 'apply', step: 'done', running: true, started_at: 'x' })),
      withState(base, (s) => (s.installer = { required: true, version: 'x', instruction: 'x', differs: [1 as unknown as string] })),
      withState(base, (s) => ((s.helper as { error: unknown }).error = { code: 5, message: 'x' })),
      withState(base, (s) => ((s.check as { error: unknown }).error = { code: 'x', message: 'x', retry_after_seconds: 'soon' })),
      withState(base, (s) => (s.last_result = { to: 'x', from: 1 as unknown as string, outcome: 'success' })),
    ];
    for (const [i, v] of bad.entries()) expect(isUpdateState(v), String(i)).toBe(false);
  });

  it('keeps a helper phase and an outcome it does not know (the page shows them neutrally)', () => {
    expect(isUpdateState(withState(upToDate(), (s) => (s.helper.phase = 'c_switching')))).toBe(true);
    expect(isUpdateState(withState(updateScenarios()['result-success']!, (s) => (s.last_result!.outcome = 'new_outcome')))).toBe(true);
  });

  it('knows exactly the steps of the service', () => {
    expect([...jobSteps]).toEqual(['checking', 'fetching_metadata', 'downloading', 'staging', 'awaiting_components_confirmation', 'activating', 'done', 'failed']);
  });
});

describe('requests', () => {
  it('reads the State and never throws: no answer, bad JSON, a wrong shape, a server error', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(json(200, available())));
    const r = await fetchUpdateState();
    expect(r.ok && r.state.available?.version).toBe('0.1.0-alpha.8');
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('offline')));
    expect(await fetchUpdateState()).toEqual({ ok: false, kind: 'network' });
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('<html>', { status: 200 })));
    expect(await fetchUpdateState()).toEqual({ ok: false, kind: 'shape' });
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(json(200, { schema_version: 1 })));
    expect(await fetchUpdateState()).toEqual({ ok: false, kind: 'shape' });
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(json(503, { error: { code: 'update_unavailable' } })));
    expect(await fetchUpdateState()).toEqual({ ok: false, kind: 'network' });
  });

  it('a 401 on a read takes the session-loss path', async () => {
    const lost = vi.fn();
    const stop = onSessionLost(lost);
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(json(401, { error: { code: 'unauthenticated' } })));
    expect(await fetchUpdateState()).toEqual({ ok: false, kind: 'session' });
    expect(lost).toHaveBeenCalledWith('expired');
    stop();
  });

  it('sends check, apply and discard as JSON with the CSRF token, and reads their answers', async () => {
    const calls: { url: string; init: RequestInit }[] = [];
    let answer = () => json(200, signedIn);
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string, init: RequestInit = {}) => {
        calls.push({ url, init });
        return answer();
      }),
    );
    await fetchSession();
    answer = () => json(200, upToDate());
    expect((await checkForUpdate(false)).ok).toBe(true);
    expect((await checkForUpdate(true)).ok).toBe(true);
    answer = () => json(202, { schema_version: 1, accepted: true });
    expect(await applyUpdate('0.1.0-alpha.8', 'pw')).toEqual({ ok: true });
    expect(await applyUpdate('0.1.0-alpha.8', 'pw', true)).toEqual({ ok: true });
    answer = () => json(200, { schema_version: 1, ok: true });
    expect(await discardUpdate('0.1.0-alpha.8')).toEqual({ ok: true });
    const posts = calls.filter((c) => c.init.method === 'POST');
    expect(posts.map((c) => [c.url, JSON.parse(String(c.init.body))])).toEqual([
      ['/api/v1/update/check', {}],
      ['/api/v1/update/check', { force: true }],
      ['/api/v1/update/apply', { version: '0.1.0-alpha.8', current_password: 'pw' }],
      ['/api/v1/update/apply', { version: '0.1.0-alpha.8', current_password: 'pw', components_confirmed: true }],
      ['/api/v1/update/discard', { version: '0.1.0-alpha.8' }],
    ]);
    for (const c of posts) {
      expect((c.init.headers as Record<string, string>)['X-Oarfish-CSRF']).toBe(sentinelCsrf);
      expect((c.init.headers as Record<string, string>)['Content-Type']).toBe('application/json');
    }
  });

  it('turns error answers into codes, an unexpected success into a shape error, and a lost session into "session"', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(json(429, { error: { code: 'rate_limited', retry_after_seconds: 8 } })));
    expect(await applyUpdate('1.0.0', 'pw')).toEqual({ ok: false, kind: 'error', code: 'rate_limited', retryAfter: 8 });
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(json(200, { schema_version: 1, accepted: true })));
    expect(await applyUpdate('1.0.0', 'pw')).toEqual({ ok: false, kind: 'shape' }); // 200 is not the 202 of the contract
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(json(200, { schema_version: 1 })));
    expect(await discardUpdate('1.0.0')).toEqual({ ok: false, kind: 'shape' });
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(json(200, { nope: true })));
    expect(await checkForUpdate(true)).toEqual({ ok: false, kind: 'shape' });
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('offline')));
    expect(await checkForUpdate(true)).toEqual({ ok: false, kind: 'network' });
    const lost = vi.fn();
    const stop = onSessionLost(lost);
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(json(401, { error: { code: 'unauthenticated' } })));
    expect(await discardUpdate('1.0.0')).toEqual({ ok: false, kind: 'session' });
    expect(lost).toHaveBeenCalledTimes(1);
    stop();
  });

  it('postJSON gives the body of any success and keeps the token private', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(json(202, { a: 1 })));
    expect(await postJSON('/api/v1/x', {})).toEqual({ ok: true, status: 202, body: { a: 1 } });
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(null, { status: 204 })));
    expect(await postJSON('/api/v1/x', {})).toEqual({ ok: true, status: 204, body: null });
  });
});

describe('error sentences of the update routes', () => {
  it('has its own sentence for every code, in both languages, and none of them is the server text', () => {
    const seen = new Map<string, string>();
    for (const code of updateCodes) {
      const text = updateErrorText(code);
      expect(text, code).toBe(de[`updErr.${code}`]);
      expect(en[`updErr.${code}`], code).toBeTruthy();
      expect(text, code).not.toBe(t.errorGeneric);
      expect(seen.has(text), `${code} shares its sentence with ${seen.get(text)}`).toBe(false);
      seen.set(text, code);
    }
    expect(updateCodes).toHaveLength(35);
  });

  it('gives every http_<three digits> one sentence and anything else the generic one', () => {
    expect(updateErrorText('http_503')).toBe(t['updErr.http']);
    expect(updateErrorText('http_404')).toBe(t['updErr.http']);
    expect(updateErrorText('http_5030')).toBe(t.errorGeneric);
    expect(updateErrorText('http_abc')).toBe(t.errorGeneric);
    expect(updateErrorText('something_else')).toBe(t.errorGeneric);
    expect(updateErrorText('')).toBe(t.errorGeneric);
  });

  it('says to check the date and time for a TLS failure', () => {
    expect(de['updErr.tls_failure']).toContain('Datum und Uhrzeit');
    expect(en['updErr.tls_failure']).toContain('date and time');
  });

  it('maps the access and body codes of the action answers', () => {
    expect(actionErrorText('wrong_password')).toEqual({ text: t.errorCurrentPassword, field: 'password' });
    expect(actionErrorText('rate_limited', 1).text).toBe(t.errorRateLimitedOneSecond);
    expect(actionErrorText('csrf_failed').text).toBe(t['updErr.request_rejected']);
    expect(actionErrorText('origin_rejected').text).toBe(t['updErr.request_rejected']);
    expect(actionErrorText('request_too_large').text).toBe(t['updErr.request_too_large']);
    expect(actionErrorText('invalid_request').text).toBe(t['updErr.bad_request']);
    expect(actionErrorText('update_unavailable').text).toBe(t['updErr.update_unavailable']);
    expect(actionErrorText('network').text).toBe(t.errorNetwork);
    expect(actionErrorText('busy')).toEqual({ text: t['updErr.busy'], field: null });
    expect(actionErrorText('unknown_code').text).toBe(t.errorGeneric);
  });

  it('says a wait in seconds below two minutes and in minutes from there', () => {
    expect(durationText(0)).toBe('einer Sekunde');
    expect(durationText(1)).toBe('einer Sekunde');
    expect(durationText(42)).toBe('42 Sekunden');
    expect(durationText(119)).toBe('119 Sekunden');
    expect(durationText(120)).toBe('2 Minuten');
    expect(durationText(1700)).toBe('29 Minuten');
  });
});
