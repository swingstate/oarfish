// The access-control pages and the session gate of the app (ADR 0009, docs/UI.md "States").
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/svelte';
import App from '../App.svelte';
import { FakeEventSource } from '../lib/eventsource.fake';
import { t } from '../lib/i18n';
import { piMetrics } from '../lib/metrics.fixtures';
import { forgetSession } from '../lib/session';
import { loginRequired, sentinelCsrf, setupRequired, signedIn } from '../lib/session.fixtures';

const sentinelPassword = 'Sentinel-Pw-3e9f1c7a';
const sentinelToken = '0123456789abcdef0123456789abcdef';

type Handler = (init: RequestInit) => Response | Promise<Response>;
const json = (status: number, body: unknown, headers: Record<string, string> = {}) =>
  new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json', ...headers } });
const apiError = (status: number, code: string, extra: Record<string, unknown> = {}) =>
  json(status, { error: { code, message: 'x', recovery: 'x', correlation_id: '0123456789abcdef', ...extra } });

let routes: Record<string, Handler>;
let calls: { key: string; init: RequestInit }[];

function serve(table: Record<string, Handler>): void {
  routes = table;
  calls = [];
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string, init: RequestInit = {}) => {
      const key = `${init.method ?? 'GET'} ${url}`;
      calls.push({ key, init });
      const h = routes[key];
      return h ? h(init) : apiError(404, 'not_found');
    }),
  );
}

const body = (key: string) => JSON.parse(String(calls.find((c) => c.key === key)!.init.body)) as Record<string, string>;
const input = (label: string) => screen.getByLabelText(label) as HTMLInputElement;
async function type(label: string, value: string) {
  await fireEvent.input(input(label), { target: { value } });
}

beforeEach(() => {
  FakeEventSource.reset();
  vi.stubGlobal('EventSource', FakeEventSource);
});
afterEach(() => {
  cleanup();
  forgetSession();
  vi.unstubAllGlobals();
  location.hash = '';
});

describe('session gate', () => {
  it('shows nothing of the dashboard before the session state is known', async () => {
    let answer!: (r: Response) => void;
    serve({ 'GET /api/v1/session': () => new Promise<Response>((resolve) => (answer = resolve)) });
    render(App);
    expect(screen.getByText(t.sessionLoading)).toBeTruthy();
    expect(screen.queryByRole('navigation')).toBeNull();
    expect(screen.queryByTestId('home')).toBeNull();
    expect(calls.map((c) => c.key)).toEqual(['GET /api/v1/session']); // no metrics, no device, no stream
    expect(FakeEventSource.instances).toHaveLength(0);
    answer(json(200, loginRequired));
    expect(await screen.findByTestId('login')).toBeTruthy();
  });

  it('says so when Oarfish cannot be reached, and tries again', async () => {
    serve({ 'GET /api/v1/session': () => Promise.reject(new TypeError('offline')) });
    render(App);
    expect(await screen.findByText(t.sessionError)).toBeTruthy();
    routes['GET /api/v1/session'] = () => json(200, setupRequired);
    await fireEvent.click(screen.getByRole('button', { name: t.homeRetry }));
    expect(await screen.findByTestId('setup')).toBeTruthy();
  });
});

describe('Setup page', () => {
  it('names the command for the token, says that Oarfish listens on this device only, and has accessible fields', async () => {
    serve({ 'GET /api/v1/session': () => json(200, setupRequired) });
    render(App);
    await screen.findByTestId('setup');
    expect(screen.getByRole('heading', { level: 1, name: t.setupTitle })).toBeTruthy();
    expect(screen.getByTestId('setup-command').textContent).toBe('sudo cat /var/lib/oarfish/setup-token');
    expect(screen.getByTestId('setup-binding').textContent).toBe(t.setupBinding);
    expect(screen.queryByRole('radio')).toBeNull(); // no binding choice yet
    const expectations: [string, string, string][] = [
      [t.setupTokenLabel, 'text', 'off'],
      [t.usernameLabel, 'text', 'username'],
      [t.passwordLabel, 'password', 'new-password'],
      [t.passwordRepeatLabel, 'password', 'new-password'],
    ];
    for (const [label, typ, ac] of expectations) {
      const el = input(label);
      expect(el.type, label).toBe(typ);
      expect(el.getAttribute('autocomplete'), label).toBe(ac);
      expect(el.getAttribute('autocapitalize'), label).toBe('off');
      expect(el.getAttribute('spellcheck'), label).toBe('false');
    }
    expect(document.activeElement).toBe(input(t.setupTokenLabel)); // focus on the first field
    expect(document.title).toBe(`${t.setupTitle} · ${t.brandName}`);
  });

  it('checks the rules before sending and names the broken one', async () => {
    serve({ 'GET /api/v1/session': () => json(200, setupRequired) });
    render(App);
    await screen.findByTestId('setup');
    await type(t.setupTokenLabel, sentinelToken);
    await type(t.usernameLabel, 'Admin');
    await type(t.passwordLabel, sentinelPassword);
    await type(t.passwordRepeatLabel, sentinelPassword);
    await fireEvent.submit(input(t.setupTokenLabel).form!);
    expect((await screen.findByRole('alert')).textContent).toContain(t.errorUsername);
    expect(document.activeElement).toBe(input(t.usernameLabel));
    expect(input(t.usernameLabel).getAttribute('aria-invalid')).toBe('true');

    await type(t.usernameLabel, 'admin-test');
    await type(t.passwordLabel, 'kurz');
    await fireEvent.submit(input(t.setupTokenLabel).form!);
    await waitFor(() => expect(screen.getByRole('alert').textContent).toContain(t.errorPasswordShort));

    await type(t.passwordLabel, sentinelPassword);
    await type(t.passwordRepeatLabel, sentinelPassword + 'x');
    await fireEvent.submit(input(t.setupTokenLabel).form!);
    await waitFor(() => expect(screen.getByRole('alert').textContent).toContain(t.errorPasswordMismatch));
    expect(input(t.passwordLabel).value).toBe(''); // cleared after every submit
    expect(input(t.passwordRepeatLabel).value).toBe('');
    expect(calls.filter((c) => c.key === 'POST /api/v1/setup')).toHaveLength(0);
  });

  it('shows the server answer for a wrong token, then sets up and opens the app', async () => {
    serve({
      'GET /api/v1/session': () => json(200, setupRequired),
      'POST /api/v1/setup': () => apiError(403, 'invalid_setup_token'),
      'GET /api/v1/metrics': () => json(200, piMetrics),
    });
    render(App);
    await screen.findByTestId('setup');
    const fill = async () => {
      await type(t.setupTokenLabel, sentinelToken);
      await type(t.usernameLabel, 'admin-test');
      await type(t.passwordLabel, sentinelPassword);
      await type(t.passwordRepeatLabel, sentinelPassword);
      await fireEvent.submit(input(t.setupTokenLabel).form!);
    };
    await fill();
    expect((await screen.findByRole('alert')).textContent).toContain(t.errorSetupToken);
    expect(document.activeElement).toBe(input(t.setupTokenLabel));
    const sent = calls.find((c) => c.key === 'POST /api/v1/setup')!;
    expect((sent.init.headers as Record<string, string>)['Content-Type']).toBe('application/json');
    expect(body('POST /api/v1/setup')).toEqual({ setup_token: sentinelToken, username: 'admin-test', password: sentinelPassword });

    routes['POST /api/v1/setup'] = () => json(200, signedIn);
    await fill();
    expect(await screen.findByTestId('home')).toBeTruthy();
  });
});

describe('Login page', () => {
  async function openLogin() {
    serve({ 'GET /api/v1/session': () => json(200, loginRequired) });
    render(App);
    await screen.findByTestId('login');
  }

  it('has labelled fields with the right autocomplete and the focus on the user name', async () => {
    await openLogin();
    expect(screen.getByRole('heading', { level: 1, name: t.loginTitle })).toBeTruthy();
    expect(input(t.usernameLabel).getAttribute('autocomplete')).toBe('username');
    expect(input(t.passwordLabel).getAttribute('autocomplete')).toBe('current-password');
    expect(input(t.passwordLabel).type).toBe('password');
    expect(document.activeElement).toBe(input(t.usernameLabel));
  });

  it('gives the same sentence for a wrong user name and a wrong password, and clears the password', async () => {
    await openLogin();
    routes['POST /api/v1/session'] = () => apiError(401, 'invalid_credentials');
    await type(t.usernameLabel, 'nobody');
    await type(t.passwordLabel, sentinelPassword);
    await fireEvent.submit(input(t.usernameLabel).form!);
    const first = (await screen.findByRole('alert')).textContent;
    expect(first).toContain(t.errorCredentials);
    expect(input(t.passwordLabel).value).toBe('');
    expect(input(t.usernameLabel).value).toBe('nobody');
    expect(document.activeElement).toBe(input(t.passwordLabel));
    await type(t.usernameLabel, 'admin-test');
    await type(t.passwordLabel, sentinelPassword + 'x');
    await fireEvent.submit(input(t.usernameLabel).form!);
    await waitFor(() => expect(calls.filter((c) => c.key === 'POST /api/v1/session')).toHaveLength(2));
    expect((await screen.findByRole('alert')).textContent).toBe(first);
  });

  it('shows the wait time when rate limited', async () => {
    await openLogin();
    routes['POST /api/v1/session'] = () => apiError(429, 'rate_limited', { retry_after_seconds: 8 });
    await type(t.usernameLabel, 'admin-test');
    await type(t.passwordLabel, sentinelPassword);
    await fireEvent.submit(input(t.usernameLabel).form!);
    expect((await screen.findByRole('alert')).textContent).toContain('8 Sekunden');
    routes['POST /api/v1/session'] = () => apiError(429, 'rate_limited', { retry_after_seconds: 900 });
    await type(t.passwordLabel, sentinelPassword);
    await fireEvent.submit(input(t.usernameLabel).form!);
    await waitFor(() => expect(screen.getByRole('alert').textContent).toContain('15 Minuten'));
  });

  it('signs in, keeps the token out of browser storage and the address, and sends no CSRF-free change', async () => {
    await openLogin();
    routes['POST /api/v1/session'] = () => json(200, signedIn);
    routes['GET /api/v1/metrics'] = () => json(200, piMetrics);
    await type(t.usernameLabel, 'admin-test');
    await type(t.passwordLabel, sentinelPassword);
    await fireEvent.submit(input(t.usernameLabel).form!);
    expect(await screen.findByTestId('home')).toBeTruthy();
    for (const where of [JSON.stringify({ ...localStorage }), JSON.stringify({ ...sessionStorage }), document.cookie, location.href, document.title]) {
      expect(where).not.toContain(sentinelCsrf);
      expect(where).not.toContain(sentinelPassword);
    }
    expect(document.body.innerHTML).not.toContain(sentinelPassword);
    expect(document.body.innerHTML).not.toContain(sentinelCsrf);
  });
});

describe('signed in', () => {
  function signedInApp(extra: Record<string, Handler> = {}) {
    serve({
      'GET /api/v1/session': () => json(200, signedIn),
      'GET /api/v1/metrics': () => json(200, piMetrics),
      ...extra,
    });
    render(App);
  }

  it('has a user menu with the account page and Abmelden, and logs out', async () => {
    signedInApp({ 'DELETE /api/v1/session': () => json(200, loginRequired) });
    await screen.findByTestId('home');
    const summary = screen.getByText('admin-test').closest('summary')!;
    expect(summary.getAttribute('aria-label')).toBe(`${t.userMenuLabel}: admin-test`);
    expect(screen.getByRole('link', { name: t.navAccount, hidden: true }).getAttribute('href')).toBe('#/account');
    await fireEvent.click(screen.getByRole('button', { name: t.logout, hidden: true }));
    expect(await screen.findByTestId('login')).toBeTruthy();
    expect(screen.getByTestId('login-notice').textContent).toContain(t.loginLoggedOut);
    const del = calls.find((c) => c.key === 'DELETE /api/v1/session')!;
    expect((del.init.headers as Record<string, string>)['X-Oarfish-CSRF']).toBe(sentinelCsrf);
    expect(FakeEventSource.instances.every((es) => es.closed)).toBe(true);
  });

  it('returns to the login page with "session ended" on a 401, without a reconnect loop', async () => {
    signedInApp({ 'GET /api/v1/metrics': () => apiError(401, 'unauthenticated') });
    expect(await screen.findByTestId('login')).toBeTruthy();
    expect(screen.getByTestId('login-notice').textContent).toContain(t.loginExpired);
    expect(FakeEventSource.instances.every((es) => es.closed)).toBe(true);
  });

  it('returns to the login page when the server ends the stream with the session event', async () => {
    signedInApp();
    await screen.findByTestId('home');
    const es = FakeEventSource.last;
    es.open();
    es.send('session', { schema_version: 1, state: 'ended' });
    expect(await screen.findByTestId('login')).toBeTruthy();
    expect(screen.getByTestId('login-notice').textContent).toContain(t.loginExpired);
    expect(es.closed).toBe(true);
    const streams = FakeEventSource.instances.length;
    await new Promise((r) => setTimeout(r, 50));
    expect(FakeEventSource.instances).toHaveLength(streams); // no new stream
  });

  it('asks for the session before reconnecting a refused stream, and stops when it is gone', async () => {
    signedInApp();
    await screen.findByTestId('home');
    routes['GET /api/v1/session'] = () => json(200, loginRequired);
    FakeEventSource.last.fail(); // the browser got a 401 and gave up
    expect(await screen.findByTestId('login')).toBeTruthy();
    expect(FakeEventSource.instances).toHaveLength(1);
  });

  it('changes the password on the account page', async () => {
    location.hash = '#/account';
    signedInApp({ 'POST /api/v1/account/password': () => apiError(403, 'wrong_password') });
    await screen.findByTestId('account');
    expect(screen.getByRole('heading', { level: 1, name: t.accountTitle })).toBeTruthy();
    expect(screen.getByTestId('account-user').textContent).toContain('admin-test');
    expect(input(t.currentPasswordLabel).getAttribute('autocomplete')).toBe('current-password');
    expect(input(t.newPasswordLabel).getAttribute('autocomplete')).toBe('new-password');
    const fill = async (current: string, next: string, repeat = next) => {
      await type(t.currentPasswordLabel, current);
      await type(t.newPasswordLabel, next);
      await type(t.newPasswordRepeatLabel, repeat);
      await fireEvent.submit(input(t.currentPasswordLabel).form!);
    };
    await fill(sentinelPassword, 'kurz');
    expect((await screen.findByRole('alert')).textContent).toContain(t.errorPasswordShort);
    await fill(sentinelPassword, 'Another-Sentinel-55');
    await waitFor(() => expect(screen.getByRole('alert').textContent).toContain(t.errorCurrentPassword));
    expect(document.activeElement).toBe(input(t.currentPasswordLabel));
    for (const l of [t.currentPasswordLabel, t.newPasswordLabel, t.newPasswordRepeatLabel]) expect(input(l).value).toBe('');
    routes['POST /api/v1/account/password'] = () => apiError(400, 'password_too_common');
    await fill(sentinelPassword, 'password1234');
    await waitFor(() => expect(screen.getByRole('alert').textContent).toContain(t.errorPasswordCommon));
    routes['POST /api/v1/account/password'] = () => json(200, signedIn);
    await fill(sentinelPassword, 'Another-Sentinel-55');
    expect((await screen.findByTestId('account-done')).textContent).toContain(t.accountChanged);
    const sent = calls.filter((c) => c.key === 'POST /api/v1/account/password').at(-1)!;
    expect((sent.init.headers as Record<string, string>)['X-Oarfish-CSRF']).toBe(sentinelCsrf);
    expect(body('POST /api/v1/account/password')).toHaveProperty('current_password');
  });
});
