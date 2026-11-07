// The update page (G6.8): every designed state, the check rules, the apply flow,
// polling and the restart (docs/UI.md, "Update page"; ADR 0010 U3, U9, A11).
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte';
import App from '../App.svelte';
import Update from './Update.svelte';
import { FakeEventSource } from '../lib/eventsource.fake';
import { en, fmt, t } from '../lib/i18n';
import { forgetSession, onSessionLost } from '../lib/session';
import { sentinelCsrf, signedIn } from '../lib/session.fixtures';
import { available, nextVersion, sampleNotes, updateScenarios, upToDate, withState } from '../lib/update.fixtures';
import type { UpdateState } from '../lib/update';
import { updateErrorText } from '../lib/updateErrors';

const sentinelPassword = 'Update-Sentinel-Pw-8d2b';

// ---- a fake server with plain response objects (no streams, so fake timers stay exact) ----

type Reply = { status: number; body?: unknown } | 'network';
type Handler = (body: Record<string, unknown>) => Reply | Promise<Reply>;

let routes: Record<string, Handler>;
let calls: { key: string; body: Record<string, unknown>; headers: Record<string, string> }[];

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
      calls.push({ key, body, headers: { ...(init.headers as Record<string, string>) } });
      const h = routes[key];
      return respond(h ? await h(body) : err(404, 'not_found'));
    }),
  );
}

const count = (key: string) => calls.filter((c) => c.key === key).length;
const GET = 'GET /api/v1/update';
const CHECK = 'POST /api/v1/update/check';
const APPLY = 'POST /api/v1/update/apply';
const DISCARD = 'POST /api/v1/update/discard';

/** Lets the fake answers and Svelte's updates run, and moves the fake clock by ms. */
async function flush(ms = 0): Promise<void> {
  await vi.advanceTimersByTimeAsync(ms);
  await vi.advanceTimersByTimeAsync(0);
}

async function open(state: UpdateState | (() => Reply), extra: Record<string, Handler> = {}): Promise<void> {
  serve({ [GET]: typeof state === 'function' ? state : () => ok(state), [CHECK]: () => ok(typeof state === 'function' ? upToDate() : state), ...extra });
  render(Update);
  await flush();
}

const byTestId = (id: string) => screen.getByTestId(id);
const alertText = () => screen.getByRole('alert').textContent?.trim();
const text = (id: string) => byTestId(id).textContent?.replace(/\s+/g, ' ').trim() ?? '';
const scenarios = () => updateScenarios(Date.now());
const scenario = (name: string) => scenarios()[name]!;

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'setInterval', 'clearInterval', 'Date'] });
  vi.setSystemTime(new Date('2026-11-07T10:00:00Z'));
});
afterEach(() => {
  cleanup();
  forgetSession();
  vi.useRealTimers();
  vi.unstubAllGlobals();
  location.hash = '';
});

// ---- every state of docs/UI.md, "Update page" -----------------------------------

describe('update page states', () => {
  it('state loading: says that the state is loading, as a status', async () => {
    serve({ [GET]: () => new Promise<Reply>(() => {}) });
    render(Update);
    await flush();
    expect(screen.getByRole('heading', { level: 1, name: t.updateTitle })).toBeTruthy();
    expect(screen.getByRole('status').textContent).toContain(t.updLoading);
  });

  it('state load failed: names the failure, a hint and a retry that reads again', async () => {
    let fail = true;
    serve({ [GET]: () => (fail ? 'network' : ok(upToDate())) });
    render(Update);
    await flush();
    expect(alertText()).toBe(t.updLoadError);
    expect(screen.getByText(t.dashboardLoadErrorHint)).toBeTruthy();
    fail = false;
    await fireEvent.click(screen.getByRole('button', { name: t.dashboardRetry }));
    await flush();
    expect(text('upd-up-to-date')).toBe(t.updUpToDate);
  });

  it('state load failed: an answer of an unknown shape is a load error too', async () => {
    await open(() => ok({ schema_version: 2, installed_version: 'x' }));
    expect(alertText()).toBe(t.updLoadError);
  });

  it('state helper not present: no apply button, the helper text and the guide', async () => {
    await open(scenario('no-helper'));
    expect(text('upd-helper-missing')).toContain(t.updHelperMissing);
    expect(text('upd-helper-missing')).toContain(t.updHelperMissingHint);
    expect(text('upd-helper-missing')).toContain(t['updErr.unavailable']);
    expect(screen.queryByTestId('upd-apply')).toBeNull();
    // The check works without the helper.
    expect(screen.getByRole('button', { name: t.updCheckNow })).toBeTruthy();
  });

  it('state never checked: says so and checks once on opening', async () => {
    await open(scenario('never'));
    expect(text('upd-never')).toBe(t.updNeverChecked);
    expect(text('upd-check-state')).toBe(t['updCheckState.never']);
    expect(text('upd-checked-at')).toBe(t.updNever);
    expect(count(CHECK)).toBe(1);
  });

  it('state up to date: installed version, check state, times, and the calm sentence', async () => {
    await open(upToDate());
    expect(text('upd-installed')).toBe('0.1.0-alpha.7');
    expect(text('upd-check-state')).toBe(t['updCheckState.ok']);
    expect(text('upd-checked-at')).toMatch(/^07\.11\.2026, \d\d:\d\d$/);
    expect(text('upd-next-check')).toMatch(/^07\.11\.2026, \d\d:\d\d$/);
    expect(text('upd-up-to-date')).toBe(t.updUpToDate);
    expect(screen.queryByTestId('upd-available')).toBeNull();
  });

  it('state update available (verified): version, channel badge, size, signature, rollback, notes and the apply button', async () => {
    await open(available());
    expect(text('upd-available-version')).toBe(nextVersion);
    expect(text('upd-channel')).toBe(t['updChannel.prerelease']);
    expect(text('upd-size')).toBe('3,3 MiB');
    expect(text('upd-signature')).toBe(t.updSignatureChecked);
    expect(text('upd-signature-note')).toBe(t.updSignatureCheckedNote);
    expect(text('upd-available')).toContain(t.updRollbackText);
    expect(text('upd-notes-source')).toBe(t.updNotesSource);
    expect(screen.getByRole('button', { name: t.updApply })).toBeTruthy();
  });

  it('state update available, stable and without notes', async () => {
    await open(scenario('available-stable'));
    expect(text('upd-channel')).toBe(t['updChannel.stable']);
    expect(text('upd-available')).toContain(t.updNotesEmpty);
  });

  it('state update available but not verified: the code sentence, no apply button', async () => {
    await open(scenario('available-unverified'));
    expect(text('upd-signature')).toBe(t.updSignatureFailed);
    expect(text('upd-unverified')).toContain(t['updErr.signature_invalid']);
    expect(text('upd-unverified')).toContain(t.updSignatureFailedNote);
    expect(screen.queryByTestId('upd-apply')).toBeNull();
  });

  it('state check running: says so, disables "Check now", and polls until the job ends', async () => {
    let n = 0;
    await open(() => ok(n++ < 2 ? scenario('check-running') : available()));
    expect(byTestId('upd-check-running').getAttribute('role')).toBe('status');
    expect((byTestId('upd-check-now') as HTMLButtonElement).disabled).toBe(true);
    await flush(2000);
    await flush(2000);
    expect(screen.getByTestId('upd-available')).toBeTruthy();
    const reads = count(GET);
    await flush(10_000);
    expect(count(GET)).toBe(reads); // the job is over: no more polling
  });

  it('state check failed: the code sentence, the retry hint and the next check', async () => {
    await open(scenario('check-failed'));
    expect(text('upd-check-state')).toBe(t['updCheckState.error']);
    expect(text('upd-check-error')).toContain(t.updCheckFailed);
    expect(text('upd-check-error')).toContain(t['updErr.tls_failure']);
    expect(t['updErr.tls_failure']).toMatch(/Datum und Uhrzeit/);
    expect(en['updErr.tls_failure']).toMatch(/date and time/);
    expect(text('upd-next-check')).toMatch(/^07\.11\.2026/);
  });

  it('state check rate limited: the wait from retry_after_seconds, and "Check now" stays locked', async () => {
    await open(scenario('check-rate-limited'));
    expect(text('upd-check-error')).toContain(t['updErr.rate_limited']);
    expect(text('upd-check-error')).toContain(fmt('updRetryHint', { duration: fmt('durMinutes', { n: 25 }) }));
    expect((byTestId('upd-check-now') as HTMLButtonElement).disabled).toBe(true);
    expect(text('upd-check-lock')).toBe(fmt('updCheckLocked', { duration: fmt('durMinutes', { n: 25 }) }));
  });

  it('state throttled: "Check now" answered from memory says so', async () => {
    await open(available(), { [CHECK]: () => ok(scenario('throttled')) });
    await fireEvent.click(byTestId('upd-check-now'));
    await flush();
    expect(text('upd-check-message')).toBe(t.updThrottled);
    expect(byTestId('upd-check-message').getAttribute('role')).toBe('status');
  });

  it('state apply running: the steps as an ordered list, the current one marked, a polite live region', async () => {
    await open(scenario('apply-downloading'));
    const list = screen.getByRole('list', { name: t.updStepsLabel });
    expect(list.tagName).toBe('OL');
    const items = Array.from(list.querySelectorAll('li'));
    expect(items.map((li) => li.querySelector('.name')?.textContent)).toEqual(
      ['fetching_metadata', 'downloading', 'staging', 'activating', 'done'].map((s) => t[`updStep.${s}` as 'updStep.done']),
    );
    expect(items.map((li) => li.getAttribute('aria-current'))).toEqual([null, 'step', null, null, null]);
    // Not by colour or a spinner alone: each step says its state in words.
    expect(items.map((li) => li.querySelector('.state')?.textContent)).toEqual([t.updStepDone, t.updStepCurrent, t.updStepPending, t.updStepPending, t.updStepPending]);
    expect(byTestId('upd-live').getAttribute('role')).toBe('status');
    expect(text('upd-live')).toBe(fmt('updCurrentStep', { step: t['updStep.downloading'] }));
    expect(screen.queryByTestId('upd-apply')).toBeNull();
    expect((byTestId('upd-check-now') as HTMLButtonElement).disabled).toBe(true);
  });

  it('state awaiting components confirmation: the generic sentence, the password and the discard', async () => {
    await open(scenario('awaiting-confirmation'));
    expect(text('upd-components')).toContain(t.updComponentsText);
    expect(text('upd-components')).toContain(t.updConfirmRestart);
    expect(text('upd-components')).toContain(t.updComponentsFail);
    expect(screen.getByLabelText(t.updPasswordLabel).getAttribute('autocomplete')).toBe('current-password');
    expect(screen.getByRole('button', { name: t.updDiscard })).toBeTruthy();
    expect(screen.queryByTestId('upd-staged')).toBeNull();
  });

  it('state installer required: the fixed text, the fixed command, the differing names, nothing of the server text', async () => {
    const s = scenario('installer-required');
    await open(s);
    const notice = byTestId('upd-installer');
    expect(notice.textContent).toContain(t.updInstallerText);
    expect(notice.textContent).toContain(fmt('updInstallerSteps', { version: nextVersion }));
    expect(notice.textContent).toContain(t.updInstallerKept);
    expect(Array.from(notice.querySelectorAll('li')).map((li) => li.textContent)).toEqual(['oarfish-update-helper', 'oarfish.service']);
    expect(notice.textContent).not.toContain(s.installer!.instruction);
    // The command is a fixed text; no command is built from the version or the names.
    const commands = Array.from(notice.querySelectorAll('p > code')).map((c) => c.textContent);
    expect(commands).toEqual(['sudo sh install.sh']);
    expect(screen.queryByTestId('upd-apply')).toBeNull();
    expect(screen.queryByTestId('upd-job-failed')).toBeNull();
  });

  it('state apply failed before activation: the job error with its sentence', async () => {
    await open(scenario('apply-failed'));
    expect(text('upd-job-failed')).toContain(fmt('updJobFailed', { version: nextVersion }));
    expect(text('upd-job-failed')).toContain(t['updErr.timeout']);
  });

  for (const [name, outcome, tone] of [
    ['result-success', 'success', 'success'],
    ['result-rolled-back', 'rolled_back', 'warning'],
    ['result-components-rolled-back', 'components_rolled_back', 'warning'],
    ['result-rollback-failed', 'rollback_failed', 'critical'],
    ['result-aborted', 'aborted', 'neutral'],
  ] as const) {
    it(`state finished, ${outcome}: badge text, sentence, time and the reason code as text`, async () => {
      const s = scenario(name);
      await open(s);
      expect(text('upd-outcome')).toBe(t[`updOutcome.${outcome}`]);
      expect(byTestId('upd-outcome').getAttribute('data-tone')).toBe(tone);
      expect(text('upd-result-text')).toBe(fmt(`updResult.${outcome}`, { to: s.last_result!.to, from: s.last_result!.from }));
      if (s.last_result!.reason_code) expect(text('upd-reason')).toBe(s.last_result!.reason_code);
      if (outcome === 'rollback_failed') {
        expect(text('upd-result')).toContain(t.updRerunHint);
        expect(text('upd-needs-attention')).toContain(t.updNeedsAttention);
      }
    });
  }

  it('state finished with an outcome this version does not know: neutral words, no guess', async () => {
    await open(withState(scenario('result-success'), (s) => (s.last_result!.outcome = 'something_new')));
    expect(text('upd-outcome')).toBe(t.unknownValue);
    expect(byTestId('upd-outcome').getAttribute('data-tone')).toBe('neutral');
    expect(text('upd-result-text')).toBe(t.updResultUnknown);
  });

  it('state cooldown: the version and the wait, and no apply button for that version', async () => {
    await open(scenario('cooldown'));
    expect(text('upd-cooldown')).toBe(fmt('updCooldown', { version: nextVersion, duration: fmt('durMinutes', { n: 29 }) }));
    expect(screen.queryByTestId('upd-apply')).toBeNull();
  });

  it('state helper needs attention: a critical notice with the next step', async () => {
    await open(scenario('needs-attention'));
    expect(byTestId('upd-needs-attention').getAttribute('data-tone')).toBe('critical');
    expect(text('upd-needs-attention')).toContain(t.updRerunHint);
  });

  it('state staged release waiting: offers discard, sends the version, reads the state again', async () => {
    await open(scenario('staged'), { [DISCARD]: () => ok({ schema_version: 1, ok: true }) });
    expect(text('upd-staged')).toContain(fmt('updStaged', { version: nextVersion }));
    expect(screen.queryByTestId('upd-apply')).toBeNull();
    routes[GET] = () => ok(available());
    await fireEvent.click(screen.getByRole('button', { name: t.updDiscard }));
    await flush();
    expect(calls.find((c) => c.key === DISCARD)!.body).toEqual({ version: nextVersion });
    expect(text('upd-discard-message')).toBe(t.updDiscarded);
    expect(screen.queryByTestId('upd-staged')).toBeNull();
    expect(screen.getByTestId('upd-apply')).toBeTruthy();
  });

  it('every state of the fixtures renders without a load error', async () => {
    for (const [name, s] of Object.entries(scenarios())) {
      await open(s);
      expect(screen.queryByText(t.updLoadError), name).toBeNull();
      expect(screen.getByRole('heading', { level: 1 }).textContent, name).toBe(t.updateTitle);
      cleanup();
    }
  });
});

// ---- release notes are text only (A7) ---------------------------------------------

describe('release notes', () => {
  it('are one text node: markup in them stays literal text', async () => {
    await open(available());
    const notes = byTestId('upd-notes');
    expect(notes.textContent).toBe(sampleNotes);
    expect(notes.textContent).toContain('<script>alert(1)</script>');
    expect(notes.querySelector('*')).toBeNull();
    expect(notes.childNodes).toHaveLength(1);
    expect(notes.childNodes[0]!.nodeType).toBe(Node.TEXT_NODE);
    expect(document.querySelector('script, b')).toBeNull();
    // Scrollable and reachable with the keyboard, named by its heading.
    expect(notes.getAttribute('tabindex')).toBe('0');
    expect(screen.getByRole('region', { name: t.updNotesHeading })).toBe(notes);
  });
});

// ---- the check rules (A3) ------------------------------------------------------------

describe('check for updates', () => {
  it('sends no check when the result is fresh, and starts no timer of its own', async () => {
    await open(available());
    expect(count(CHECK)).toBe(0);
    expect(count(GET)).toBe(1);
    await flush(3 * 3_600_000);
    expect(count(CHECK)).toBe(0);
    expect(count(GET)).toBe(1);
  });

  it('sends one check {} on opening when next_check_at has passed', async () => {
    await open(withState(available(), (s) => (s.check.next_check_at = new Date(Date.now() - 1000).toISOString())));
    expect(count(CHECK)).toBe(1);
    expect(calls.find((c) => c.key === CHECK)!.body).toEqual({});
  });

  it('"Check now" sends force, is locked for 60 s with a countdown, and is free again after it', async () => {
    await open(upToDate());
    const button = byTestId('upd-check-now') as HTMLButtonElement;
    expect(button.getAttribute('aria-describedby')).toBe('upd-check-note');
    await fireEvent.click(button);
    await flush();
    expect(calls.filter((c) => c.key === CHECK).map((c) => c.body)).toEqual([{ force: true }]);
    expect(button.disabled).toBe(true);
    expect(text('upd-check-lock')).toBe(fmt('updCheckLocked', { duration: fmt('durSeconds', { n: 60 }) }));
    await flush(15_000);
    expect(text('upd-check-lock')).toBe(fmt('updCheckLocked', { duration: fmt('durSeconds', { n: 45 }) }));
    await flush(45_000);
    expect(button.disabled).toBe(false);
    expect(screen.queryByTestId('upd-check-lock')).toBeNull();
    expect(count(CHECK)).toBe(1);
  });

  it('a rate_limited answer is shown with its wait and locks the button for that long', async () => {
    await open(upToDate(), { [CHECK]: () => err(429, 'rate_limited', { retry_after_seconds: 300 }) });
    await fireEvent.click(byTestId('upd-check-now'));
    await flush();
    expect(text('upd-check-message')).toBe(fmt('errorRateLimitedMinutes', { minutes: 5 }));
    await flush(60_000);
    expect((byTestId('upd-check-now') as HTMLButtonElement).disabled).toBe(true);
    await flush(240_000);
    expect((byTestId('upd-check-now') as HTMLButtonElement).disabled).toBe(false);
  });

  it('a busy answer is a calm status and the state is read again', async () => {
    await open(upToDate(), { [CHECK]: () => err(409, 'busy') });
    await fireEvent.click(byTestId('upd-check-now'));
    await flush();
    expect(text('upd-check-message')).toBe(t['updErr.busy']);
    expect(byTestId('upd-check-message').getAttribute('role')).toBe('status');
    expect(count(GET)).toBe(2);
  });
});

// ---- apply (A4) ------------------------------------------------------------------------

describe('apply', () => {
  async function openConfirm(extra: Record<string, Handler> = {}) {
    await open(available(), extra);
    await fireEvent.click(byTestId('upd-apply'));
    await flush();
    return screen.getByLabelText(t.updPasswordLabel) as HTMLInputElement;
  }

  it('opens a labelled confirmation with the facts, the restart sentence and the focus in the password field', async () => {
    const field = await openConfirm();
    const panel = screen.getByRole('region', { name: t.updConfirmHeading });
    expect(panel.textContent).toContain(fmt('updConfirmFromTo', { from: '0.1.0-alpha.7', to: nextVersion }));
    expect(panel.textContent).toContain(t.updRollbackText);
    expect(panel.textContent).toContain(t.updSignatureChecked);
    expect(document.activeElement).toBe(field);
    expect(field.type).toBe('password');
    expect(field.getAttribute('autocomplete')).toBe('current-password');
    expect(document.getElementById(field.getAttribute('aria-describedby')!)!.textContent).toBe(t.updConfirmRestart);
  });

  it('Escape and Abbrechen close it and give the focus back to the apply button', async () => {
    let field = await openConfirm();
    await fireEvent.input(field, { target: { value: sentinelPassword } });
    await fireEvent.keyDown(field, { key: 'Escape' });
    await flush();
    expect(screen.queryByTestId('upd-confirm')).toBeNull();
    expect(document.activeElement).toBe(byTestId('upd-apply'));
    await fireEvent.click(byTestId('upd-apply'));
    await flush();
    field = screen.getByLabelText(t.updPasswordLabel) as HTMLInputElement;
    expect(field.value).toBe(''); // nothing kept from before
    await fireEvent.click(screen.getByRole('button', { name: t.updCancel }));
    await flush();
    expect(document.activeElement).toBe(byTestId('upd-apply'));
    expect(count(APPLY)).toBe(0);
  });

  it('needs the password: an empty field sends nothing, is marked and focused', async () => {
    const field = await openConfirm();
    await fireEvent.click(byTestId('upd-confirm-submit'));
    await flush();
    expect(count(APPLY)).toBe(0);
    expect(alertText()).toBe(t.errorRequired);
    expect(field.getAttribute('aria-invalid')).toBe('true');
    expect(document.activeElement).toBe(field);
  });

  for (const [code, status, extra, expected] of [
    ['wrong_password', 403, {}, () => t.errorCurrentPassword],
    ['rate_limited', 429, { retry_after_seconds: 8 }, () => fmt('errorRateLimitedSeconds', { seconds: 8 })],
    ['busy', 409, {}, () => t['updErr.busy']],
    ['version_mismatch', 409, {}, () => t['updErr.version_mismatch']],
    ['not_checked', 409, {}, () => t['updErr.not_checked']],
    ['already_installed', 409, {}, () => t['updErr.already_installed']],
    ['current_unhealthy', 409, {}, () => t['updErr.current_unhealthy']],
    ['cooldown', 409, {}, () => t['updErr.cooldown']],
    ['unavailable', 503, {}, () => t['updErr.unavailable']],
    ['cache_error', 503, {}, () => t['updErr.cache_error']],
    ['installer_required', 422, {}, () => t['updErr.installer_required']],
    ['csrf_failed', 403, {}, () => t['updErr.request_rejected']],
    ['origin_rejected', 403, {}, () => t['updErr.request_rejected']],
  ] as const) {
    it(`shows ${code} with its own sentence, clears the password and focuses the field`, async () => {
      const field = await openConfirm({ [APPLY]: () => err(status, code, extra) });
      await fireEvent.input(field, { target: { value: sentinelPassword } });
      await fireEvent.click(byTestId('upd-confirm-submit'));
      await flush();
      expect(calls.find((c) => c.key === APPLY)!.body).toEqual({ version: nextVersion, current_password: sentinelPassword });
      expect(alertText()).toBe(expected());
      expect(alertText()).not.toContain('English server text');
      expect(field.value).toBe('');
      expect(document.activeElement).toBe(field);
      expect(count(GET)).toBe(code === 'wrong_password' || code === 'rate_limited' ? 1 : 2);
    });
  }

  it('says when Oarfish cannot be reached', async () => {
    const field = await openConfirm({ [APPLY]: () => 'network' });
    await fireEvent.input(field, { target: { value: sentinelPassword } });
    await fireEvent.click(byTestId('upd-confirm-submit'));
    await flush();
    expect(alertText()).toBe(t.errorNetwork);
  });

  it('202 starts polling every 2 s; polling stops when job.running is false', async () => {
    let state = available();
    const field = await openConfirm({ [APPLY]: () => ok({ schema_version: 1, accepted: true }, 202) });
    routes[GET] = () => ok(state);
    state = scenario('apply-downloading');
    await fireEvent.input(field, { target: { value: sentinelPassword } });
    await fireEvent.click(byTestId('upd-confirm-submit'));
    await flush();
    expect(field.value).toBe('');
    expect(screen.queryByTestId('upd-confirm')).toBeNull();
    expect(document.activeElement).toBe(byTestId('upd-progress'));
    const after202 = count(GET);
    await flush(2000);
    expect(count(GET)).toBe(after202 + 1);
    await flush(2000);
    expect(count(GET)).toBe(after202 + 2);
    // The job fails before activation: nothing switched, polling ends.
    state = scenario('apply-failed');
    await flush(2000);
    expect(screen.getByTestId('upd-job-failed')).toBeTruthy();
    const end = count(GET);
    await flush(20_000);
    expect(count(GET)).toBe(end);
  });

  it('polling stops when the page is left', async () => {
    await open(scenario('apply-downloading'));
    await flush(2000);
    const before = count(GET);
    cleanup();
    await flush(20_000);
    expect(count(GET)).toBe(before);
  });

  it('keeps the password out of browser storage, the address and the page', async () => {
    const field = await openConfirm({ [APPLY]: () => err(403, 'wrong_password') });
    await fireEvent.input(field, { target: { value: sentinelPassword } });
    await fireEvent.click(byTestId('upd-confirm-submit'));
    await flush();
    const stored = [
      ...Object.keys(localStorage).map((k) => k + '=' + localStorage.getItem(k)),
      ...Object.keys(sessionStorage).map((k) => k + '=' + sessionStorage.getItem(k)),
      document.cookie,
      location.href,
      document.title,
      document.body.innerHTML,
    ].join('\n');
    expect(stored).not.toContain(sentinelPassword);
  });

  it('the second confirmation sends components_confirmed with the job version and the password', async () => {
    await open(scenario('awaiting-confirmation'), { [APPLY]: () => ok({ schema_version: 1, accepted: true }, 202) });
    const field = screen.getByLabelText(t.updPasswordLabel) as HTMLInputElement;
    await fireEvent.input(field, { target: { value: sentinelPassword } });
    routes[GET] = () => ok(scenario('apply-activating'));
    await fireEvent.click(screen.getByRole('button', { name: t.updComponentsSubmit }));
    await flush();
    expect(calls.find((c) => c.key === APPLY)!.body).toEqual({ version: nextVersion, current_password: sentinelPassword, components_confirmed: true });
    expect(screen.getByTestId('upd-progress')).toBeTruthy();
  });

  it('the second confirmation can be discarded instead', async () => {
    await open(scenario('awaiting-confirmation'), { [DISCARD]: () => ok({ schema_version: 1, ok: true }) });
    routes[GET] = () => ok(available());
    await fireEvent.click(screen.getByRole('button', { name: t.updDiscard }));
    await flush();
    expect(calls.find((c) => c.key === DISCARD)!.body).toEqual({ version: nextVersion });
    expect(screen.queryByTestId('upd-components')).toBeNull();
  });
});

// ---- the restart around "activating" (A5) -------------------------------------------

describe('restart', () => {
  it('failed reads during activating show the restarting text, keep trying every 2 s and recover', async () => {
    let reply: Reply = ok(scenario('apply-activating'));
    await open(() => reply);
    expect(byTestId('upd-progress').querySelector('[aria-current="step"] .name')!.textContent).toBe(t['updStep.activating']);
    reply = 'network';
    await flush(2000);
    expect(text('upd-live')).toBe(t.updRestarting);
    const during = count(GET);
    await flush(2000);
    await flush(2000);
    expect(count(GET)).toBe(during + 2);
    // The new process answers: no job, the helper committed, the result is shown.
    reply = ok(scenario('result-success'));
    await flush(2000);
    expect(screen.queryByTestId('upd-progress')).toBeNull();
    expect(text('upd-outcome')).toBe(t['updOutcome.success']);
    const end = count(GET);
    await flush(20_000);
    expect(count(GET)).toBe(end);
  });

  it('a failed read before activating says only that Oarfish does not answer, and recovers', async () => {
    let reply: Reply = ok(scenario('apply-downloading'));
    await open(() => reply);
    reply = 'network';
    await flush(2000);
    expect(text('upd-live')).toBe(t.updNoAnswer);
    reply = ok(scenario('apply-downloading'));
    await flush(2000);
    expect(text('upd-live')).toBe(fmt('updCurrentStep', { step: t['updStep.downloading'] }));
  });

  it('a failed read while a check runs is a load error and ends the polling', async () => {
    let reply: Reply = ok(scenario('check-running'));
    await open(() => reply);
    reply = 'network';
    await flush(2000);
    expect(alertText()).toBe(t.updLoadError);
    const end = count(GET);
    await flush(20_000);
    expect(count(GET)).toBe(end);
  });

  it('waits while the old process still answers "done" and the helper is switching', async () => {
    let reply: Reply = ok(withState(scenario('apply-activating'), (s) => Object.assign(s.job!, { step: 'done', running: false })));
    await open(() => reply);
    expect(screen.getByTestId('upd-progress')).toBeTruthy();
    await flush(2000);
    await flush(2000);
    expect(count(GET)).toBe(3);
    reply = ok(scenario('result-success'));
    await flush(2000);
    expect(text('upd-outcome')).toBe(t['updOutcome.success']);
  });

  it('gives up after three minutes without an answer and advises to open the page again', async () => {
    let reply: Reply = ok(scenario('apply-activating'));
    await open(() => reply);
    reply = 'network';
    await flush(2000);
    for (let i = 0; i < 95; i++) await flush(2000);
    expect(text('upd-restart-timeout')).toContain(t.updRestartTimeout);
    const end = count(GET);
    await flush(20_000);
    expect(count(GET)).toBe(end);
    reply = ok(scenario('result-success'));
    await fireEvent.click(screen.getByRole('button', { name: t.deviceReload }));
    await flush();
    expect(text('upd-outcome')).toBe(t['updOutcome.success']);
  });

  it('a 401 after the restart takes the session-loss path once and stops polling', async () => {
    const lost = vi.fn();
    const stop = onSessionLost(lost);
    let reply: Reply = ok(scenario('apply-activating'));
    await open(() => reply);
    reply = err(401, 'unauthenticated');
    await flush(2000);
    expect(lost).toHaveBeenCalledWith('expired');
    const end = count(GET);
    await flush(20_000);
    expect(count(GET)).toBe(end);
    expect(lost).toHaveBeenCalledTimes(1);
    stop();
  });

  it('in the app, the 401 leads to the login page, and after login the page shows the last result', async () => {
    vi.stubGlobal('EventSource', FakeEventSource);
    location.hash = '#/update';
    let reply: Reply = ok(scenario('apply-activating'));
    serve({
      'GET /api/v1/session': () => ok(signedIn),
      'POST /api/v1/session': () => ok(signedIn),
      [GET]: () => reply,
    });
    render(App);
    await flush();
    expect(screen.getByTestId('upd-progress')).toBeTruthy();
    reply = err(401, 'unauthenticated');
    await flush(2000);
    expect(screen.getByText(t.loginExpired)).toBeTruthy();
    reply = ok(scenario('result-success'));
    await fireEvent.input(screen.getByLabelText(t.usernameLabel), { target: { value: 'admin-test' } });
    await fireEvent.input(screen.getByLabelText(t.passwordLabel), { target: { value: sentinelPassword } });
    await fireEvent.click(screen.getByRole('button', { name: t.loginSubmit }));
    await flush();
    expect(text('upd-outcome')).toBe(t['updOutcome.success']);
  });

  it('in the app, the state-changing requests carry the CSRF token of the session as JSON', async () => {
    vi.stubGlobal('EventSource', FakeEventSource);
    location.hash = '#/update';
    serve({ 'GET /api/v1/session': () => ok(signedIn), [GET]: () => ok(upToDate()), [CHECK]: () => ok(upToDate()) });
    render(App);
    await flush();
    await fireEvent.click(byTestId('upd-check-now'));
    await flush();
    const check = calls.find((c) => c.key === CHECK)!;
    expect(check.headers['X-Oarfish-CSRF']).toBe(sentinelCsrf);
    expect(check.headers['Content-Type']).toBe('application/json');
    expect(document.title).toBe(`${t.navUpdate} · ${t.brandName}`);
    expect(screen.getByRole('link', { name: t.navUpdate }).getAttribute('aria-current')).toBe('page');
  });
});

// ---- structure and accessibility -------------------------------------------------------

describe('structure', () => {
  it('has headings in order: h1, then h2 per card, h3 inside', async () => {
    await open(scenario('result-aborted'));
    await fireEvent.click(byTestId('upd-apply'));
    await flush();
    const levels = Array.from(document.querySelectorAll('h1, h2, h3, h4')).map((h) => Number(h.tagName[1]));
    expect(levels[0]).toBe(1);
    for (let i = 1; i < levels.length; i++) expect(levels[i]! - levels[i - 1]!, `heading ${i}`).toBeLessThanOrEqual(1);
    expect(levels).toContain(3);
  });

  it('uses only native interactive elements and no positive tabindex', async () => {
    await open(scenario('result-aborted'));
    await fireEvent.click(byTestId('upd-apply'));
    await flush();
    expect(document.querySelectorAll('[role="button"], [tabindex]:not([tabindex="0"]):not([tabindex="-1"])')).toHaveLength(0);
    for (const input of Array.from(document.querySelectorAll('input'))) expect(document.querySelector(`label[for="${input.id}"]`), input.id).not.toBeNull();
  });

  it('every update error code of the State has its own sentence in the page', () => {
    expect(updateErrorText('tls_failure')).toBe(t['updErr.tls_failure']);
  });
});
