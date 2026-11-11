// Development scenario mode for the update page (G6.8, docs/DEVELOPMENT.md).
//
// Only in the development server (import.meta.env.DEV) and only when the address
// has ?mock=<scenario>: a fake fetch layer answers the session route and the four
// update routes from the fixtures, so every state of the page can be seen without
// a device. Every other request passes through unchanged. The production build
// does not contain this file: main.ts imports it in a branch that Vite removes,
// and the build fails if the marker below appears in the output (vite.config.ts).
import { available, nextVersion, updateScenarios, withState } from '../lib/update.fixtures';
import type { JobStep, UpdateState } from '../lib/update';

/** A unique string: the production build is checked for it. */
export const devMockMarker = 'oarfish-dev-mock';

type Reply = UpdateState | 'network' | 'unauthorized' | 'hang';

interface Mock {
  /** The answers of GET /api/v1/update, in order; the last one stays. */
  gets: Reply[];
  signedOut: boolean;
}

const jobAt = (s: UpdateState, step: JobStep, phase = 'idle'): UpdateState =>
  withState(s, (x) => {
    x.job = { kind: 'apply', step, version: nextVersion, running: true, started_at: new Date().toISOString() };
    x.helper.phase = phase;
  });

/** What the page sees after "Aktualisieren": the steps, the restart (no answer, then the session is gone), the result. */
function applyTimeline(from: UpdateState, components = false): Reply[] {
  const all = updateScenarios();
  if (components) {
    // After the second confirmation (G6.10): the component phases, the hand-over of the helper
    // (F4: no answer from the helper, shown as progress), the release phases, the restart, the result.
    const done = all['apply-components-phase']!;
    const at = (phase: string) => withState(done, (x) => (x.helper = { present: true, phase }));
    return [at('c_switching'), all['apply-handing-over']!, all['apply-handing-over']!, at('c_switched'), at('c_committed'), at('switching'), at('verifying'), 'network', 'network', 'unauthorized', all['result-success-components']!];
  }
  const steps: Reply[] = [jobAt(from, 'fetching_metadata'), jobAt(from, 'downloading'), jobAt(from, 'downloading'), jobAt(from, 'staging', 'staging')];
  return [...steps, jobAt(from, 'activating', 'switching'), jobAt(from, 'activating', 'verifying'), 'network', 'network', 'network', 'unauthorized', all['result-success']!];
}

/** The scenarios: every state of docs/UI.md, "Update page", plus a few with a course over time. */
export function scenarioNames(): string[] {
  return [...Object.keys(updateScenarios()), 'loading', 'load-error', 'check-running-then-done', 'apply-flow', 'components-flow', 'restart-timeout'];
}

function build(name: string): Reply[] | null {
  const all = updateScenarios();
  const av = available();
  switch (name) {
    case 'loading':
      return ['hang'];
    case 'load-error':
      return ['network'];
    case 'check-running-then-done':
      return [all['check-running']!, all['check-running']!, av];
    case 'apply-flow':
      return applyTimeline(av);
    case 'components-flow':
      // The pause for the second confirmation; "Bestätigen und aktualisieren" continues with applyTimeline(…, true).
      return [all['awaiting-confirmation']!];
    case 'restart-timeout':
      return [jobAt(av, 'activating', 'switching'), 'network'];
  }
  if (name === 'apply-downloading') return applyTimeline(av).slice(1);
  if (name === 'apply-activating') return applyTimeline(av).slice(4);
  return all[name] ? [all[name]] : null;
}

const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
const apiError = (status: number, code: string, retry?: number) =>
  json({ error: { code, message: 'mock', recovery: 'mock', correlation_id: 'mock', ...(retry ? { retry_after_seconds: retry } : {}) } }, status);

const session = { schema_version: 1, state: 'authenticated', username: 'admin-test', csrf_token: 'mock-csrf', idle_timeout_seconds: 3600 };

function later<T>(value: T, signal?: AbortSignal | null): Promise<T> {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => resolve(value), 150);
    signal?.addEventListener('abort', () => {
      clearTimeout(timer);
      reject(new DOMException('aborted', 'AbortError'));
    });
  });
}

function head(m: Mock): Reply {
  return m.gets.length > 1 ? m.gets.shift()! : m.gets[0]!;
}

async function answer(m: Mock, method: string, path: string, body: Record<string, unknown>, signal?: AbortSignal | null): Promise<Response | null> {
  if (path === '/api/v1/session') {
    if (method === 'POST') m.signedOut = false;
    if (method === 'DELETE') m.signedOut = true;
    return later(json(m.signedOut ? { schema_version: 1, state: 'login_required' } : session), signal);
  }
  if (path === '/api/v1/update' && method === 'GET') {
    const r = head(m);
    if (r === 'hang') return new Promise((_, reject) => signal?.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError'))));
    if (r === 'network') return later(null, signal).then(() => Promise.reject(new TypeError('mock: no answer')));
    if (r === 'unauthorized') {
      m.signedOut = true;
      return later(apiError(401, 'unauthenticated'), signal);
    }
    return later(json(r), signal);
  }
  const current = m.gets.find((g): g is UpdateState => typeof g === 'object') ?? available();
  if (path === '/api/v1/update/check' && method === 'POST') {
    return later(json(withState(current, (s) => (s.check.throttled = body.force === true && s.check.throttled === true))), signal);
  }
  if (path === '/api/v1/update/apply' && method === 'POST') {
    // A few passwords show the error answers; any other password is accepted.
    if (body.current_password === 'falsch' || body.current_password === 'wrong') return later(apiError(403, 'wrong_password'), signal);
    if (body.current_password === 'viele') return later(apiError(429, 'rate_limited', 8), signal);
    if (body.current_password === 'busy') return later(apiError(409, 'busy'), signal);
    m.gets = applyTimeline(available(), body.components_confirmed === true);
    return later(json({ schema_version: 1, accepted: true }, 202), signal);
  }
  if (path === '/api/v1/update/discard' && method === 'POST') {
    m.gets = [withState(current, (s) => {
      s.job = null;
      s.helper = { present: true, phase: 'idle' };
    })];
    return later(json({ schema_version: 1, ok: true }), signal);
  }
  return null;
}

/** Replaces window.fetch for the session and update routes. Unknown scenario: a console hint, nothing replaced. */
export function installUpdateMock(name: string): void {
  const gets = build(name);
  if (!gets) {
    console.warn(`[${devMockMarker}] unknown scenario "${name}". Known: ${scenarioNames().join(', ')}`);
    return;
  }
  const m: Mock = { gets, signedOut: false };
  const real = window.fetch.bind(window);
  window.fetch = async (input: RequestInfo | URL, init?: RequestInit) => {
    const href = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
    const url = new URL(href, location.href);
    const method = (init?.method ?? (input instanceof Request ? input.method : 'GET')).toUpperCase();
    let body: Record<string, unknown> = {};
    try {
      body = typeof init?.body === 'string' && init.body !== '' ? (JSON.parse(init.body) as Record<string, unknown>) : {};
    } catch {
      body = {};
    }
    if (url.origin === location.origin) {
      const r = await answer(m, method, url.pathname, body, init?.signal);
      if (r) return r;
    }
    return real(input, init);
  };
  console.info(`[${devMockMarker}] scenario "${name}"`);
}
