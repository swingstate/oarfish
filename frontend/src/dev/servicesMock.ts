// Development scenario mode for the Activity page (G7.4, docs/DEVELOPMENT.md).
//
// Only in the development server (import.meta.env.DEV) and only when the address
// has ?mock=<scenario>: a fake fetch layer answers the session route and the
// three services routes from the fixtures, so every state of the page can be
// seen without a device. Every other request passes through unchanged. The
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

/** A unique string: the production build is checked for it. */
export const devMockMarker = 'oarfish-dev-mock';

type ListReply = ServicesList | 'hang' | 'network' | 'systemd' | 'unauthorized';

interface Scenario {
  list: ListReply;
  /** What the action reads answer after the owner clicks an action, in order; the last one stays. */
  after: (unit: string, action: string) => ActionState[];
}

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
  const m: Mock = { s: make(), signedOut: false, actions: [noAction] };
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
