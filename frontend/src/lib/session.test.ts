import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  changePassword,
  codePoints,
  fetchSession,
  forgetSession,
  handleUnauthorized,
  hasCsrfToken,
  login,
  logout,
  onSessionLost,
  passwordProblem,
  sessionUser,
} from './session';
import { loginRequired, sentinelCsrf, signedIn } from './session.fixtures';

const json = (status: number, body: unknown, headers: Record<string, string> = {}) =>
  new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json', ...headers } });

afterEach(() => {
  forgetSession();
  vi.unstubAllGlobals();
});

describe('session client', () => {
  it('reads the session state and keeps the CSRF token in memory only', async () => {
    const r = await fetchSession(vi.fn(async () => json(200, signedIn)));
    expect(r.ok && r.session.state).toBe('authenticated');
    expect(hasCsrfToken()).toBe(true);
    expect(sessionUser()).toBe('admin-test');
    expect(JSON.stringify({ ...localStorage })).not.toContain(sentinelCsrf);
    expect(JSON.stringify({ ...sessionStorage })).not.toContain(sentinelCsrf);
    expect(document.cookie).not.toContain(sentinelCsrf);
    expect(location.href).not.toContain(sentinelCsrf);

    await fetchSession(vi.fn(async () => json(200, loginRequired)));
    expect(hasCsrfToken()).toBe(false);
    expect((await fetchSession(vi.fn(async () => json(200, { state: 'authenticated' })))).ok).toBe(false);
    expect((await fetchSession(vi.fn().mockRejectedValue(new TypeError('offline')))).ok).toBe(false);
  });

  it('sends JSON and the CSRF header with every state-changing request', async () => {
    await fetchSession(vi.fn(async () => json(200, signedIn)));
    const fetchFn = vi.fn(async () => json(200, signedIn));
    await changePassword('old-sentinel-pw-1', 'new-sentinel-pw-2', fetchFn);
    const [url, init] = fetchFn.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe('/api/v1/account/password');
    expect(init.method).toBe('POST');
    const headers = init.headers as Record<string, string>;
    expect(headers['Content-Type']).toBe('application/json');
    expect(headers['X-Oarfish-CSRF']).toBe(sentinelCsrf);
    expect(JSON.parse(String(init.body))).toEqual({ current_password: 'old-sentinel-pw-1', new_password: 'new-sentinel-pw-2' });
    expect(url).not.toContain('sentinel');
  });

  it('reports errors with their code and wait time, and only a lost session as a lost session', async () => {
    const lost = vi.fn();
    const stop = onSessionLost(lost);
    const wrong = await login('admin-test', 'wrong-sentinel-pw', vi.fn(async () => json(401, { error: { code: 'invalid_credentials' } })));
    expect(wrong).toEqual({ ok: false, code: 'invalid_credentials', retryAfter: null });
    expect(lost).not.toHaveBeenCalled();
    const limited = await login('admin-test', 'x', vi.fn(async () => json(429, { error: { code: 'rate_limited' } }, { 'Retry-After': '8' })));
    expect(limited).toEqual({ ok: false, code: 'rate_limited', retryAfter: 8 });
    const offline = await login('admin-test', 'x', vi.fn().mockRejectedValue(new TypeError('offline')));
    expect(offline).toEqual({ ok: false, code: 'network', retryAfter: null });
    await changePassword('a', 'b', vi.fn(async () => json(401, { error: { code: 'unauthenticated' } })));
    expect(lost).toHaveBeenCalledWith('expired');
    expect(await handleUnauthorized(json(401, { error: { code: 'setup_required' } }))).toBe(true);
    expect(lost).toHaveBeenLastCalledWith('setup_required');
    expect(await handleUnauthorized(json(200, {}))).toBe(false);
    stop();
  });

  it('forgets the token on logout, whatever the server answers', async () => {
    await fetchSession(vi.fn(async () => json(200, signedIn)));
    await logout(vi.fn().mockRejectedValue(new TypeError('offline')));
    expect(hasCsrfToken()).toBe(false);
    expect(sessionUser()).toBeNull();
  });

  it('counts password length in code points, as the server does', () => {
    expect(codePoints('🐟🐟🐟')).toBe(3);
    expect(passwordProblem('äöüßäöüßäöü')).toBe('password_too_short'); // 11
    expect(passwordProblem('äöüßäöüßäöüß')).toBeNull(); // 12
    expect(passwordProblem('x'.repeat(129))).toBe('password_too_long');
  });
});
