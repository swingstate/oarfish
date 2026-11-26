// Restart and shutdown on the Activity page (G7.5; docs/UI.md, "Restart and
// shutdown"; ADR 0011 B4, B6): the card, the native modal dialog and its focus
// rules, the waiting list, the password kept only in the input and the one
// request, every refusal as one sentence, the status view that asks only
// /healthz, both languages, and the power scenarios of the development mode.
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte';
import Activity from './Activity.svelte';
import ActivityPower from './ActivityPower.svelte';
import PowerStatus from './PowerStatus.svelte';
import App from '../App.svelte';
import { fmt, t } from '../lib/i18n';
import { setLang } from '../lib/lang.svelte';
import { forgetSession, onSessionLost } from '../lib/session';
import { onPowerAccepted } from '../lib/power';
import { powerCodes } from '../lib/powerErrors';
import { everyWaitingKind, powerState } from '../lib/power.fixtures';
import { noAction, sampleList } from '../lib/services.fixtures';
import { signedIn } from '../lib/session.fixtures';
import { installServicesMock } from '../dev/servicesMock';
import { FakeEventSource } from '../lib/eventsource.fake';

// jsdom has no showModal and close. The stand-in sets and removes the open
// attribute as a browser does; the focus trap is the browser's and is not
// tested here (docs/UI.md, Verification status).
const dialogCalls: string[] = [];
beforeAll(() => {
  const proto = HTMLDialogElement.prototype as unknown as { showModal: () => void; close: () => void };
  proto.showModal = function (this: HTMLDialogElement) {
    dialogCalls.push('showModal');
    this.setAttribute('open', '');
  };
  proto.close = function (this: HTMLDialogElement) {
    dialogCalls.push('close');
    this.removeAttribute('open');
    this.dispatchEvent(new Event('close'));
  };
});

// ---- a fake server with plain response objects ----

type Reply = { status: number; body?: unknown } | 'network' | Promise<{ status: number; body?: unknown } | 'network'>;
type Handler = (body: Record<string, unknown>) => Reply;

let routes: Record<string, Handler>;
let calls: { key: string; body: string }[];

const ok = (body: unknown, status = 200) => ({ status, body });
const err = (status: number, code: string, extra: Record<string, unknown> = {}) => ({
  status,
  body: { error: { code, message: 'English server text that must not be shown', recovery: 'x', correlation_id: '0123456789abcdef', ...extra } },
});

function respond(r: { status: number; body?: unknown } | 'network'): Response {
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
      const raw = typeof init.body === 'string' ? init.body : '';
      calls.push({ key, body: raw });
      const h = routes[key];
      return respond(h ? await h(raw ? (JSON.parse(raw) as Record<string, unknown>) : {}) : err(404, 'not_found'));
    }),
  );
}

const STATE = 'GET /api/v1/power';
const POST = 'POST /api/v1/power';
const HEALTH = 'GET /healthz';
const count = (key: string) => calls.filter((c) => c.key === key).length;
const MARKER = 'Fake-Sentinel-Pw-7c2e';

async function flush(ms = 0): Promise<void> {
  await vi.advanceTimersByTimeAsync(ms);
  await vi.advanceTimersByTimeAsync(0);
}

async function openCard(extra: Record<string, Handler> = {}, state = powerState()): Promise<void> {
  serve({ [STATE]: () => ok(state), ...extra });
  render(ActivityPower);
  await flush();
}

const byTestId = (id: string) => screen.getByTestId(id);
const text = (el: Element | null) => el?.textContent?.replace(/\s+/g, ' ').trim() ?? '';
const dialog = () => byTestId('pwr-dialog') as HTMLDialogElement;
const pw = () => screen.queryByTestId('pwr-password') as HTMLInputElement | null;
const confirmButton = () => byTestId('pwr-confirm') as HTMLButtonElement;

async function openDialog(which: 'reboot' | 'poweroff' = 'reboot'): Promise<void> {
  await fireEvent.click(byTestId(`pwr-open-${which}`));
  await flush();
}

async function typePassword(value = MARKER): Promise<void> {
  const input = pw()!;
  input.value = value;
  await fireEvent.input(input);
}

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'setInterval', 'clearInterval', 'Date'] });
  vi.setSystemTime(new Date('2026-11-25T10:00:00Z'));
  dialogCalls.length = 0;
});
afterEach(() => {
  cleanup();
  forgetSession();
  setLang('de');
  vi.useRealTimers();
  vi.unstubAllGlobals();
  location.hash = '';
});

describe('restart and shutdown card', () => {
  it('one card with its h2, the intro and two native buttons; GET /api/v1/power once on opening, nothing else', async () => {
    await openCard();
    const card = byTestId('pwr-card');
    expect(card.querySelector('h2')?.textContent).toContain(t.pwrHeading);
    expect(text(card)).toContain(t.pwrIntro);
    expect(text(byTestId('pwr-open-reboot'))).toBe(t.pwrReboot);
    expect(text(byTestId('pwr-open-poweroff'))).toBe(t.pwrPoweroff);
    for (const b of card.querySelectorAll('button')) {
      expect(b.getAttribute('type')).toBe('button');
      expect((b as HTMLButtonElement).disabled).toBe(false);
    }
    expect(calls.map((c) => c.key)).toEqual([STATE]);
    expect(dialog().hasAttribute('open')).toBe(false);
    expect(pw()).toBeNull();
  });

  it('on the Activity page: below the services, one h1 on the page', async () => {
    serve({ 'GET /api/v1/services': () => ok(sampleList()), 'GET /api/v1/services/action': () => ok(noAction), [STATE]: () => ok(powerState()) });
    render(Activity);
    await flush();
    expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1);
    const page = byTestId('activity');
    const services = page.querySelector('[aria-labelledby="services-heading"]')!;
    const power = byTestId('pwr-card');
    expect(services.compareDocumentPosition(power) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it('broker unavailable: the notice, both buttons disabled, a click opens nothing and sends nothing', async () => {
    await openCard({}, powerState([], false));
    expect(text(byTestId('pwr-broker-missing'))).toBe(t['pwrErr.unavailable']);
    for (const id of ['pwr-open-reboot', 'pwr-open-poweroff']) expect((byTestId(id) as HTMLButtonElement).disabled).toBe(true);
    await fireEvent.click(byTestId('pwr-open-reboot'));
    await flush();
    expect(dialogCalls).toEqual([]);
    expect(calls.map((c) => c.key)).toEqual([STATE]);
  });
});

describe('restart and shutdown dialog', () => {
  it('reboot: a native modal dialog with the title, the body and the tunnel sentence; the focus is on the title', async () => {
    await openCard();
    const opener = byTestId('pwr-open-reboot');
    opener.focus();
    await openDialog('reboot');
    expect(dialogCalls).toEqual(['showModal']);
    const d = dialog();
    expect(d.tagName).toBe('DIALOG');
    expect(d.hasAttribute('open')).toBe(true);
    const title = d.querySelector('h2')!;
    expect(title.textContent).toBe(t.pwrRebootTitle);
    expect(d.getAttribute('aria-labelledby')).toBe(title.id);
    expect(document.activeElement).toBe(title);
    expect(text(byTestId('pwr-body'))).toBe(t.pwrRebootBody);
    expect(text(byTestId('pwr-tunnel'))).toBe(t.pwrTunnel);
    expect(text(confirmButton())).toBe(t.pwrReboot);
  });

  it('shutdown: its own title, body and confirm button', async () => {
    await openCard();
    await openDialog('poweroff');
    expect(dialog().querySelector('h2')!.textContent).toBe(t.pwrPoweroffTitle);
    expect(text(byTestId('pwr-body'))).toBe(t.pwrPoweroffBody);
    expect(text(byTestId('pwr-tunnel'))).toBe(t.pwrTunnel);
    expect(text(confirmButton())).toBe(t.pwrPoweroff);
  });

  it('a labelled password field (type password, autocomplete current-password, help tied to it)', async () => {
    await openCard();
    await openDialog();
    const input = pw()!;
    expect(input.type).toBe('password');
    expect(input.getAttribute('autocomplete')).toBe('current-password');
    expect(screen.getByLabelText(t.updPasswordLabel)).toBe(input);
    expect(input.getAttribute('aria-describedby')).toBe('pwr-password-help');
    expect(text(document.getElementById('pwr-password-help'))).toBe(t.pwrPasswordHelp);
    expect(dialog().querySelector('[role="button"], [tabindex]:not([tabindex="-1"]), select')).toBeNull();
  });

  it('the waiting list is read when the dialog opens: loading, then "Keine wartenden Aktionen"', async () => {
    let release!: () => void;
    await openCard({ [STATE]: () => (count(STATE) === 1 ? ok(powerState()) : new Promise((r) => (release = () => r(ok(powerState()))))) });
    await openDialog();
    expect(count(STATE)).toBe(2);
    expect(text(byTestId('pwr-waiting'))).toContain(t.pwrWaitingLoading);
    release();
    await flush();
    expect(text(byTestId('pwr-waiting'))).toContain(t.pwrWaitingNone);
    expect(byTestId('pwr-waiting').querySelector('h3')?.textContent).toBe(t.pwrWaitingHeading);
  });

  it('the waiting list names every kind in words, an unknown one neutrally, never the raw word', async () => {
    const items = [...everyWaitingKind(), { kind: '<img src=x onerror=alert(1)>' }];
    await openCard({}, powerState(items));
    await openDialog();
    const lis = [...byTestId('pwr-waiting').querySelectorAll('li')].map((li) => li.textContent);
    expect(lis).toEqual([
      t['pwrWait.update_staged'],
      t['pwrWait.update_running'],
      t['pwrWait.service_action_running'],
      fmt('pwrWait.other_sessions.many', { count: 2 }),
      fmt('pwrWait.other_views.many', { count: 3 }),
      t['pwrWait.other'],
      t['pwrWait.other'],
    ]);
    expect(dialog().querySelector('img')).toBeNull();
    expect(text(dialog())).not.toContain('future_kind');
    expect(text(dialog())).not.toContain('onerror');
  });

  it('the waiting list could not be read: says so, and the dialog still works', async () => {
    await openCard({ [STATE]: () => (count(STATE) === 1 ? ok(powerState()) : 'network'), [POST]: () => ok({ schema_version: 1, accepted: true }) });
    await openDialog();
    expect(text(byTestId('pwr-waiting'))).toContain(t.pwrWaitingError);
    await typePassword();
    expect(confirmButton().disabled).toBe(false);
  });

  it('the confirm button is disabled while the field is empty and while the request runs', async () => {
    let release!: () => void;
    await openCard({ [POST]: () => new Promise((r) => (release = () => r(err(403, 'wrong_password')))) });
    await openDialog();
    expect(confirmButton().disabled).toBe(true);
    await typePassword('');
    expect(confirmButton().disabled).toBe(true);
    await typePassword();
    expect(confirmButton().disabled).toBe(false);
    await fireEvent.submit(pw()!.form!);
    await flush();
    expect(confirmButton().disabled).toBe(true);
    expect(text(confirmButton())).toBe(t.pwrWorking);
    expect((byTestId('pwr-cancel') as HTMLButtonElement).disabled).toBe(true);
    expect(pw()!.disabled).toBe(true);
    // A second submit while it runs sends nothing.
    await fireEvent.submit(pw()!.form!);
    await flush();
    expect(count(POST)).toBe(1);
    release();
    await flush();
    expect(confirmButton().disabled).toBe(true); // the field was cleared
  });

  it('Abbrechen and Escape close it, clear the password and give the focus back to the button that opened it', async () => {
    await openCard();
    for (const [which, how] of [
      ['reboot', 'cancel'],
      ['poweroff', 'escape'],
      ['reboot', 'native-cancel'],
    ] as const) {
      const opener = byTestId(`pwr-open-${which}`);
      opener.focus();
      await openDialog(which);
      const input = pw()!;
      await typePassword();
      if (how === 'cancel') await fireEvent.click(byTestId('pwr-cancel'));
      else if (how === 'escape') await fireEvent.keyDown(dialog(), { key: 'Escape' });
      else await fireEvent(dialog(), new Event('cancel', { cancelable: true }));
      await flush();
      expect(dialog().hasAttribute('open'), how).toBe(false);
      expect(input.value, how).toBe('');
      expect(pw(), how).toBeNull();
      expect(document.activeElement, how).toBe(opener);
    }
    expect(count(POST)).toBe(0);
  });

  it('Escape does nothing while the request runs', async () => {
    let release!: () => void;
    await openCard({ [POST]: () => new Promise((r) => (release = () => r(err(409, 'busy')))) });
    await openDialog();
    await typePassword();
    await fireEvent.submit(pw()!.form!);
    await flush();
    await fireEvent.keyDown(dialog(), { key: 'Escape' });
    await flush();
    expect(dialog().hasAttribute('open')).toBe(true);
    release();
    await flush();
    expect(dialog().hasAttribute('open')).toBe(true);
  });

  it('sends the action and the password in one request, clears the field, closes and reports accepted', async () => {
    const accepted = vi.fn();
    const off = onPowerAccepted(accepted);
    await openCard({ [POST]: () => ok({ schema_version: 1, accepted: true }) });
    await openDialog('poweroff');
    const input = pw()!;
    await typePassword();
    await fireEvent.submit(input.form!);
    await flush();
    expect(calls.filter((c) => c.key === POST).map((c) => JSON.parse(c.body))).toEqual([{ action: 'poweroff', current_password: MARKER }]);
    expect(input.value).toBe('');
    expect(dialog().hasAttribute('open')).toBe(false);
    expect(accepted.mock.calls).toEqual([['poweroff']]);
    off();
  });

  it('every refusal is one sentence in the dialog, tied to the field; the dialog stays open and the field is cleared', async () => {
    const cases: [ReturnType<typeof err> | 'network', string, boolean][] = [
      [err(403, 'wrong_password'), t.errorCurrentPassword, true],
      [err(429, 'rate_limited', { retry_after_seconds: 37 }), fmt('errorRateLimitedSeconds', { seconds: 37 }), false],
      ...powerCodes.map((c) => [err(c === 'unavailable' ? 503 : 409, c), t[`pwrErr.${c}`], false] as [ReturnType<typeof err>, string, boolean]),
      [err(403, 'csrf_failed'), t['updErr.request_rejected'], false],
      [err(502, 'something_new'), t.errorGeneric, false],
      ['network', t.errorNetwork, false],
    ];
    for (const [reply, sentence, invalid] of cases) {
      cleanup();
      await openCard({ [POST]: () => reply });
      await openDialog();
      await typePassword();
      await fireEvent.submit(pw()!.form!);
      await flush();
      const e = byTestId('pwr-error');
      expect(text(e)).toBe(sentence);
      expect(e.getAttribute('role')).toBe('alert');
      expect(dialog().hasAttribute('open')).toBe(true);
      const input = pw()!;
      expect(input.value).toBe('');
      expect(input.getAttribute('aria-describedby')).toBe('pwr-password-help pwr-error');
      expect(input.getAttribute('aria-invalid')).toBe(invalid ? 'true' : null);
      expect(document.activeElement).toBe(input);
      expect(text(dialog())).not.toContain('English server text');
    }
  });

  it('unavailable from the route also disables the buttons of the card', async () => {
    await openCard({ [POST]: () => err(503, 'unavailable') });
    await openDialog();
    await typePassword();
    await fireEvent.submit(pw()!.form!);
    await flush();
    await fireEvent.click(byTestId('pwr-cancel'));
    await flush();
    expect(byTestId('pwr-broker-missing')).toBeTruthy();
    expect((byTestId('pwr-open-reboot') as HTMLButtonElement).disabled).toBe(true);
  });

  it('a 401 takes the existing session-loss path', async () => {
    const lost = vi.fn();
    const off = onSessionLost(lost);
    await openCard({ [POST]: () => err(401, 'unauthenticated') });
    await openDialog();
    await typePassword();
    await fireEvent.submit(pw()!.form!);
    await flush();
    expect(lost).toHaveBeenCalledWith('expired');
    off();
  });

  it('the password is never in storage, the address, the page or a request other than the one POST', async () => {
    await openCard({ [POST]: () => err(403, 'wrong_password') });
    await openDialog();
    await typePassword();
    await fireEvent.submit(pw()!.form!);
    await flush();
    await typePassword();
    await fireEvent.click(byTestId('pwr-cancel'));
    await flush();
    expect(JSON.stringify({ ...localStorage })).not.toContain(MARKER);
    expect(JSON.stringify({ ...sessionStorage })).not.toContain(MARKER);
    expect(location.href).not.toContain(MARKER);
    expect(document.body.innerHTML).not.toContain(MARKER);
    expect(document.title).not.toContain(MARKER);
    const carrying = calls.filter((c) => c.body.includes(MARKER) || c.key.includes(MARKER));
    expect(carrying.map((c) => c.key)).toEqual([POST]);
  });

  it('works without a native showModal (an older browser): the open attribute instead', async () => {
    const proto = HTMLDialogElement.prototype as unknown as Record<string, unknown>;
    const saved = { showModal: proto.showModal, close: proto.close };
    delete proto.showModal;
    delete proto.close;
    try {
      await openCard();
      await openDialog();
      expect(dialog().hasAttribute('open')).toBe(true);
      await fireEvent.click(byTestId('pwr-cancel'));
      await flush();
      expect(dialog().hasAttribute('open')).toBe(false);
    } finally {
      Object.assign(proto, saved);
    }
  });

  it('English: the same dialog in the other language', async () => {
    setLang('en');
    await openCard({}, powerState([{ kind: 'update_staged' }]));
    await openDialog('poweroff');
    expect(dialog().querySelector('h2')!.textContent).toBe('Shut down the device?');
    expect(text(byTestId('pwr-tunnel'))).toBe('If you are connected through an SSH tunnel, it is closed. Open it again afterwards.');
    expect(text(byTestId('pwr-waiting'))).toContain('A prepared update will be discarded.');
    expect(text(confirmButton())).toBe('Shut down');
  });
});

describe('power status view', () => {
  function healthSequence(seq: (boolean | 'hang')[]): void {
    let i = 0;
    serve({
      [HEALTH]: () => {
        const v = seq[Math.min(i++, seq.length - 1)];
        return v === 'hang' ? new Promise(() => {}) : v ? ok({ status: 'ok' }) : 'network';
      },
    });
  }
  const statusText = () => text(byTestId('pwr-status-text'));

  it('restart: one h1 with the focus, one polite live region, the tunnel sentence; /healthz every 5 s; back after a failure, then the link and no more polls', async () => {
    healthSequence([true, false, false, true]);
    render(PowerStatus, { action: 'reboot' });
    await flush();
    expect(screen.getAllByRole('heading', { level: 1 }).map((h) => h.textContent)).toEqual([t.pwrStatusReboot]);
    expect(document.activeElement).toBe(screen.getByRole('heading', { level: 1 }));
    expect(screen.getAllByRole('status')).toHaveLength(1);
    expect(byTestId('pwr-status-text').getAttribute('aria-live')).toBe('polite');
    expect(text(byTestId('pwr-status-tunnel'))).toBe(t.pwrTunnel);
    expect(statusText()).toBe(t.pwrDuringReboot);
    expect(count(HEALTH)).toBe(0);
    await flush(5000);
    expect(count(HEALTH)).toBe(1);
    expect(statusText()).toBe(t.pwrDuringReboot); // answered: not restarted yet
    await flush(5000);
    expect(statusText()).toBe(t.pwrDuringReboot); // the first failure: the same text
    await flush(5000);
    expect(statusText()).toBe(t.pwrDuringReboot);
    expect(screen.queryByTestId('pwr-sign-in')).toBeNull();
    await flush(5000);
    expect(count(HEALTH)).toBe(4);
    expect(statusText()).toBe(t.pwrAfterReboot);
    const link = byTestId('pwr-sign-in') as HTMLAnchorElement;
    expect(text(link)).toBe(t.pwrSignIn);
    expect(link.getAttribute('href')).toBe(location.pathname + location.search);
    expect(text(byTestId('pwr-status-tunnel'))).toBe(t.pwrTunnel);
    await flush(60_000);
    expect(count(HEALTH)).toBe(4);
    expect(calls.every((c) => c.key === HEALTH)).toBe(true);
  });

  it('restart: after 5 minutes without an answer it says so, keeps asking, and an answer still brings the link', async () => {
    let back = false;
    serve({ [HEALTH]: () => (back ? ok({ status: 'ok' }) : 'network') });
    render(PowerStatus, { action: 'reboot' });
    await flush();
    await flush(4 * 60_000 + 55_000);
    expect(statusText()).toBe(t.pwrDuringReboot);
    await flush(5000);
    expect(statusText()).toBe(t.pwrRebootLate);
    expect(byTestId('pwr-status').getAttribute('data-phase')).toBe('late');
    const before = count(HEALTH);
    await flush(10_000);
    expect(count(HEALTH)).toBe(before + 2);
    back = true;
    await flush(5000);
    expect(statusText()).toBe(t.pwrAfterReboot);
    expect(byTestId('pwr-sign-in')).toBeTruthy();
  });

  it('shutdown: "fährt herunter", then at the first failed answer the expected sentence, and the polling stops', async () => {
    healthSequence([true, true, false, true]);
    render(PowerStatus, { action: 'poweroff' });
    await flush();
    expect(screen.getByRole('heading', { level: 1 }).textContent).toBe(t.pwrStatusPoweroff);
    expect(statusText()).toBe(t.pwrDuringPoweroff);
    await flush(10_000);
    expect(statusText()).toBe(t.pwrDuringPoweroff);
    await flush(5000);
    expect(statusText()).toBe(t.pwrAfterPoweroff);
    expect(text(byTestId('pwr-status-tunnel'))).toBe(t.pwrTunnel);
    expect(screen.queryByTestId('pwr-sign-in')).toBeNull();
    await flush(60_000);
    expect(count(HEALTH)).toBe(3);
  });

  it('a poll that hangs is given up by its own timeout and counts as no answer', async () => {
    healthSequence(['hang']);
    render(PowerStatus, { action: 'poweroff' });
    await flush(5000);
    // AbortSignal.timeout is not faked; the fake server never answers, so the
    // text stays until the probe gives up (in a browser after 4 s).
    expect(statusText()).toBe(t.pwrDuringPoweroff);
    expect(count(HEALTH)).toBe(1);
  });

  it('the component cleanup stops the polling, and so does following the link', async () => {
    healthSequence([false]);
    const view = render(PowerStatus, { action: 'reboot' });
    await flush(10_000);
    const n = count(HEALTH);
    view.unmount();
    await flush(60_000);
    expect(count(HEALTH)).toBe(n);

    healthSequence([false, true]);
    render(PowerStatus, { action: 'reboot' });
    await flush(10_000);
    const link = byTestId('pwr-sign-in');
    link.addEventListener('click', (e) => e.preventDefault()); // jsdom does not navigate
    await fireEvent.click(link);
    await flush(60_000);
    expect(count(HEALTH)).toBe(2);
  });

  it('English: the same view in the other language', async () => {
    setLang('en');
    healthSequence([false, true]);
    render(PowerStatus, { action: 'reboot' });
    await flush();
    expect(screen.getByRole('heading', { level: 1 }).textContent).toBe('Restart requested');
    expect(statusText()).toBe('The device is restarting …');
    await flush(10_000);
    expect(statusText()).toBe('The device can be reached again. Sign in.');
  });
});

describe('the whole app after accepted', () => {
  beforeEach(() => {
    FakeEventSource.reset();
    vi.stubGlobal('EventSource', FakeEventSource);
  });

  it('the status view replaces the shell: no navigation, no page, and only /healthz is requested', async () => {
    location.hash = '#/activity';
    let health = 0;
    serve({
      'GET /api/v1/session': () => ok(signedIn),
      'GET /api/v1/services': () => ok(sampleList()),
      'GET /api/v1/services/action': () => ok(noAction),
      [STATE]: () => ok(powerState()),
      [POST]: () => ok({ schema_version: 1, accepted: true }),
      [HEALTH]: () => (++health > 2 ? 'network' : ok({ status: 'ok', version: '0.0.0', pid: 1 })),
    });
    render(App);
    await flush(100);
    expect(byTestId('activity')).toBeTruthy();
    expect(screen.getByRole('navigation')).toBeTruthy();
    await openDialog('reboot');
    await typePassword();
    await fireEvent.submit(pw()!.form!);
    await flush();
    const at = calls.length;
    expect(calls[at - 1]!.key).toBe(POST);
    expect(screen.queryByRole('navigation')).toBeNull();
    expect(screen.queryByTestId('activity')).toBeNull();
    expect(byTestId('pwr-status')).toBeTruthy();
    expect(screen.getAllByRole('heading', { level: 1 }).map((h) => h.textContent)).toEqual([t.pwrStatusReboot]);
    expect(document.title).toBe(`${t.pwrStatusReboot} · ${t.brandName}`);
    // Navigation by address changes nothing now.
    location.hash = '#/update';
    window.dispatchEvent(new HashChangeEvent('hashchange'));
    await flush(60_000);
    const after = calls.slice(at).map((c) => c.key);
    expect(after.length).toBeGreaterThan(5);
    expect(new Set(after)).toEqual(new Set([HEALTH]));
    expect(byTestId('pwr-status')).toBeTruthy();
    expect(FakeEventSource.instances.every((es) => es.closed)).toBe(true);
  });
});

describe('power scenario mode', () => {
  const names = [
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
  ];

  for (const name of names) {
    it(`scenario ${name} shows its state, and nothing leaves the mock`, async () => {
      const real = vi.fn(async () => new Response('{}', { status: 404 }));
      vi.stubGlobal('fetch', real);
      const lost = vi.fn();
      const offLost = onSessionLost(lost);
      let accepted: string | null = null;
      const offAccepted = onPowerAccepted((a) => (accepted = a));
      expect(installServicesMock(name)).toBe(true);
      render(Activity);
      await flush(500);
      if (name === 'power-broker-unavailable') {
        expect(byTestId('pwr-broker-missing')).toBeTruthy();
        expect((byTestId('pwr-open-reboot') as HTMLButtonElement).disabled).toBe(true);
      } else {
        const which = name.includes('shutdown') ? 'poweroff' : 'reboot';
        await fireEvent.click(byTestId(`pwr-open-${which}`));
        await flush(500);
        expect(dialog().hasAttribute('open')).toBe(true);
        const waiting = text(byTestId('pwr-waiting'));
        if (name === 'power-waiting-list') expect(waiting).toContain(fmt('pwrWait.other_sessions.many', { count: 2 }));
        else if (name === 'power-update-running') expect(waiting).toContain(t['pwrWait.update_running']);
        else expect(waiting).toContain(t.pwrWaitingNone);
        await typePassword();
        await fireEvent.submit(pw()!.form!);
        await flush(500);
        const want: Record<string, string> = {
          'power-wrong-password': t.errorCurrentPassword,
          'power-rate-limited': fmt('errorRateLimitedSeconds', { seconds: 37 }),
          'power-update-running': t['pwrErr.update_running'],
          'power-already-requested': t['pwrErr.already_requested'],
        };
        if (want[name]) expect(text(byTestId('pwr-error'))).toBe(want[name]);
        else if (name === 'power-session-lost') expect(lost).toHaveBeenCalled();
        else expect(accepted).toBe(which);
      }
      // The mock answered everything itself: no request reached the real fetch.
      expect(real).not.toHaveBeenCalled();
      offLost();
      offAccepted();
    });
  }

  it('the flow scenarios run their course in the status view, with /healthz from the mock', async () => {
    for (const [name, action, steps, want] of [
      ['power-reboot-flow', 'reboot', 6, t.pwrAfterReboot],
      ['power-reboot-too-long', 'reboot', 61, t.pwrRebootLate],
      ['power-shutdown-flow', 'poweroff', 2, t.pwrAfterPoweroff],
    ] as const) {
      cleanup();
      const real = vi.fn(async () => new Response('{}', { status: 404 }));
      vi.stubGlobal('fetch', real);
      expect(installServicesMock(name)).toBe(true);
      // Accept through the mock, then show the view as App.svelte does.
      const pending = fetch('/api/v1/power', { method: 'POST', body: JSON.stringify({ action, current_password: 'x' }) });
      await flush(200);
      expect((await pending).status).toBe(200);
      render(PowerStatus, { action });
      for (let i = 0; i < steps; i++) await flush(5150); // the interval plus the mock's 150 ms answer time
      await flush(500);
      expect(text(byTestId('pwr-status-text')), name).toBe(want);
      expect(real).not.toHaveBeenCalled();
    }
  });
});
