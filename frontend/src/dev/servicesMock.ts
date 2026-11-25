// Development scenario mode for the Activity page (G7.4 and G7.5,
// docs/DEVELOPMENT.md).
//
// Only in the development server (import.meta.env.DEV) and only when the address
// has ?mock=<scenario>: a fake fetch layer answers the session route, the three
// services routes, the power routes and /healthz from the fixtures, so every
// state of the page can be seen without a device. The power routes and
// /healthz are always answered here in a scenario of this file: no scenario
// sends a reboot or shutdown anywhere, and the status view never reaches a real
// device. Every other request passes through unchanged. The
// production build does not contain this file: main.ts imports it in a branch
// that Vite removes, and the build fails if the marker below or the name of the
// installer appears in the output (vite.config.ts). Synthetic data only.
import {
  brokerMissingList,
  done,
  failedList,
  longList,
  noAction,
  noFailedList,
  running,
  sampleList,
} from '../lib/services.fixtures';
import type { ActionState, ServicesList } from '../lib/services';
import { everyWaitingKind, powerState } from '../lib/power.fixtures';
import type { PowerState } from '../lib/power';

/** A unique string: the production build is checked for it. */
export const devMockMarker = 'oarfish-dev-mock';

type ListReply = ServicesList | 'hang' | 'network' | 'systemd' | 'unauthorized';

/** The answer of POST /api/v1/power: accepted, or a typed error. */
type PowerReply = 'accepted' | { status: number; code: string; retryAfter?: number };

interface PowerScenario {
  /** GET /api/v1/power; null: derived from the list (its broker_available, nothing waiting). */
  state: PowerState | null;
  post: PowerReply;
  /** The answers of GET /healthz after an accepted request, one per poll (5 s); the last one stays. */
  health: boolean[];
}

interface Scenario {
  list: ListReply;
  /** What the action reads answer after the owner clicks an action, in order; the last one stays. */
  after: (unit: string, action: string) => ActionState[];
  power?: Partial<PowerScenario>;
}

/** A restart: two answers before the service stops, three failed polls, then the device is back. */
const rebootHealth = [true, true, false, false, false, true];

const flow = (code: string, extra: Record<string, unknown> = {}) => (unit: string, action: string) => [
  running(unit, action),
  running(unit, action),
  running(unit, action),
  done(unit, action, code, extra),
];

const success = (unit: string, action: string) =>
  flow('success', { state: action === 'stop' ? 'inactive' : 'active' })(unit, action);

/** The scenarios of docs/UI.md, "Activity page". */
const scenarios: Record<string, () => Scenario> = {
  loading: () => ({ list: 'hang', after: success }),
  'load-error': () => ({ list: 'network', after: success }),
  'systemd-unavailable': () => ({ list: 'systemd', after: success }),
  list: () => ({ list: sampleList(), after: success }),
  'list-long': () => ({ list: longList(), after: success }),
  'list-filtered-empty': () => ({ list: noFailedList(), after: success }),
  'list-with-failed': () => ({ list: failedList(), after: success }),
  'broker-unavailable': () => ({ list: brokerMissingList(), after: success }),
  'action-flow': () => ({ list: sampleList(), after: success }),
  'action-timeout': () => ({ list: sampleList(), after: flow('timeout', { state: 'activating' }) }),
  'action-rate-limited': () => ({ list: sampleList(), after: flow('rate_limited', { retry_after_s: 37 }) }),
  'action-oneshot-refused': () => ({ list: sampleList(), after: flow('unit_oneshot') }),
  'action-restart-only-note': () => ({ list: sampleList(), after: success }),
  'action-denied-row': () => ({ list: sampleList(), after: success }),
  'session-lost': () => ({ list: 'unauthorized', after: success }),
  // ---- restart and shutdown (G7.5) ----
  'power-dialog-reboot': () => ({ list: sampleList(), after: success }),
  'power-dialog-shutdown': () => ({ list: sampleList(), after: success }),
  'power-waiting-list': () => ({ list: sampleList(), after: success, power: { state: powerState(everyWaitingKind()) } }),
  'power-wrong-password': () => ({ list: sampleList(), after: success, power: { post: { status: 403, code: 'wrong_password' } } }),
  'power-rate-limited': () => ({ list: sampleList(), after: success, power: { post: { status: 429, code: 'rate_limited', retryAfter: 37 } } }),
  'power-update-running': () => ({
    list: sampleList(),
    after: success,
    power: { state: powerState([{ kind: 'update_running' }]), post: { status: 409, code: 'update_running' } },
  }),
  'power-already-requested': () => ({ list: sampleList(), after: success, power: { post: { status: 409, code: 'already_requested' } } }),
  'power-broker-unavailable': () => ({ list: brokerMissingList(), after: success }),
  'power-reboot-flow': () => ({ list: sampleList(), after: success, power: { health: rebootHealth } }),
  'power-reboot-too-long': () => ({ list: sampleList(), after: success, power: { health: [true, false] } }),
  'power-shutdown-flow': () => ({ list: sampleList(), after: success, power: { health: [true, false] } }),
  'power-session-lost': () => ({ list: sampleList(), after: success, power: { post: { status: 401, code: 'unauthenticated' } } }),
};

/** The names, for the console hint and the documentation. */
export function servicesScenarioNames(): string[] {
  return Object.keys(scenarios);
}

const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
const apiError = (status: number, code: string) => json({ error: { code, message: 'mock', recovery: 'mock', correlation_id: 'mock' } }, status);

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

interface Mock {
  s: Scenario;
  signedOut: boolean;
  actions: ActionState[];
  /** GET /healthz answers still to come after an accepted power request; null before it. */
  health: boolean[] | null;
}

function powerOf(s: Scenario): PowerScenario {
  const l = s.list;
  const derived = powerState([], typeof l === 'object' ? l.broker_available : true);
  return { state: s.power?.state ?? derived, post: s.power?.post ?? 'accepted', health: s.power?.health ?? rebootHealth };
}

async function answer(m: Mock, method: string, path: string, body: Record<string, unknown>, signal?: AbortSignal | null): Promise<Response | null> {
  if (path === '/api/v1/session') {
    if (method === 'POST') m.signedOut = false;
    if (method === 'DELETE') m.signedOut = true;
    return later(json(m.signedOut ? { schema_version: 1, state: 'login_required' } : session), signal);
  }
  if (path === '/api/v1/services' && method === 'GET') {
    const l = m.s.list;
    if (l === 'hang') return new Promise((_, reject) => signal?.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError'))));
    if (l === 'network') return later(null, signal).then(() => Promise.reject(new TypeError('mock: no answer')));
    if (l === 'systemd') return later(apiError(503, 'services_unavailable'), signal);
    if (l === 'unauthorized') {
      m.signedOut = true;
      return later(apiError(401, 'unauthenticated'), signal);
    }
    return later(json({ ...l, read_at: new Date().toISOString() }), signal);
  }
  if (path === '/api/v1/services/action' && method === 'GET') {
    const next = m.actions.length > 1 ? m.actions.shift()! : (m.actions[0] ?? noAction);
    return later(json(next), signal);
  }
  if (path === '/api/v1/power' && method === 'GET') return later(json(powerOf(m.s).state), signal);
  if (path === '/api/v1/power' && method === 'POST') {
    const p = powerOf(m.s);
    if (p.state && !p.state.broker_available) return later(apiError(503, 'unavailable'), signal);
    // The password is not looked at, kept or shown: any text stands for the right one.
    if (p.post === 'accepted') {
      m.health = [...p.health];
      return later(json({ schema_version: 1, accepted: true }), signal);
    }
    if (p.post.status === 401) m.signedOut = true;
    const res = apiError(p.post.status, p.post.code);
    if (p.post.retryAfter === undefined) return later(res, signal);
    return later(json({ error: { code: p.post.code, message: 'mock', recovery: 'mock', correlation_id: 'mock', retry_after_seconds: p.post.retryAfter } }, p.post.status), signal);
  }
  if (path === '/healthz' && method === 'GET') {
    // Before an accepted request the device answers; afterwards the scenario's course.
    const next = m.health === null ? true : m.health.length > 1 ? m.health.shift()! : (m.health[0] ?? true);
    if (!next) return later(null, signal).then(() => Promise.reject(new TypeError('mock: no answer')));
    return later(json({ status: 'ok', version: '0.0.0-mock', pid: 1 }), signal);
  }
  if (path === '/api/v1/services/action' && method === 'POST') {
    const l = m.s.list;
    if (typeof l === 'object' && !l.broker_available) return later(apiError(503, 'unavailable'), signal);
    if (m.actions.length > 1) return later(apiError(409, 'busy'), signal);
    m.actions = m.s.after(String(body.unit), String(body.action));
    return later(json({ schema_version: 1, accepted: true }, 202), signal);
  }
  return null;
}

/** Replaces window.fetch for the session and services routes. Unknown scenario: nothing is replaced. */
export function installServicesMock(name: string): boolean {
  const make = scenarios[name];
  if (!make) return false;
  const m: Mock = { s: make(), signedOut: false, actions: [noAction], health: null };
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
  console.info(`[${devMockMarker}] services scenario "${name}"`);
  return true;
}
