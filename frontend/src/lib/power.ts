// Client for the power routes of the Activity page (ADR 0011 B4, B6, step
// G7.5; docs/UI.md, "Restart and shutdown"). Every answer is checked at run
// time: a shape this version does not expect is a load error. A kind of
// waiting action this version does not know is kept and shown neutrally. The
// password is a parameter of requestPower only: it is never kept here, never
// logged and never part of a URL. Nothing here throws.
import { handleUnauthorized, postJSON } from './session';

/** The two power actions of the route. */
export const powerActions = ['reboot', 'poweroff'] as const;
export type PowerAction = (typeof powerActions)[number];

/** The kinds of waiting actions the service sends (a closed set of the service). */
export const waitingKinds = ['update_staged', 'update_running', 'service_action_running', 'other_sessions', 'other_views'] as const;

export interface WaitingItem {
  kind: string;
  /** Only for other_sessions and other_views; at least 1. */
  count?: number;
}

/** GET /api/v1/power (schema_version 1). */
export interface PowerState {
  schema_version: 1;
  broker_available: boolean;
  waiting: WaitingItem[];
}

type Obj = Record<string, unknown>;

const isObj = (v: unknown): v is Obj => typeof v === 'object' && v !== null && !Array.isArray(v);

function isWaitingItem(v: unknown): v is WaitingItem {
  return (
    isObj(v) &&
    typeof v.kind === 'string' &&
    v.kind !== '' &&
    (v.count === undefined || (typeof v.count === 'number' && Number.isInteger(v.count) && v.count >= 1))
  );
}

/** Whether a value is the power state of schema version 1. */
export function isPowerState(v: unknown): v is PowerState {
  return isObj(v) && v.schema_version === 1 && typeof v.broker_available === 'boolean' && Array.isArray(v.waiting) && v.waiting.every(isWaitingItem);
}

/**
 * A failure. `session`: the session is gone and the app has been told.
 * `network`: no answer. `shape`: an answer this version cannot read.
 * `unavailable`: this build has no power routes. `error`: a typed error code.
 */
export type PowerFailure =
  | { ok: false; kind: 'session' }
  | { ok: false; kind: 'network' }
  | { ok: false; kind: 'shape' }
  | { ok: false; kind: 'unavailable' }
  | { ok: false; kind: 'error'; code: string; retryAfter: number | null };

export type PowerStateResult = { ok: true; state: PowerState } | PowerFailure;
export type PowerRequestResult = { ok: true } | PowerFailure;

async function codeOf(res: Response): Promise<string> {
  try {
    const body = (await res.json()) as { error?: { code?: unknown } };
    return typeof body.error?.code === 'string' ? body.error.code : '';
  } catch {
    return '';
  }
}

/** GET /api/v1/power: the broker's socket and the waiting actions. It may read the update helper's status (at most 3 s). */
export async function fetchPower(fetchFn: typeof fetch = fetch, timeoutMs = 10000): Promise<PowerStateResult> {
  let res: Response;
  try {
    res = await fetchFn('/api/v1/power', { cache: 'no-store', credentials: 'same-origin', signal: AbortSignal.timeout(timeoutMs) });
  } catch {
    return { ok: false, kind: 'network' };
  }
  try {
    if (await handleUnauthorized(res)) return { ok: false, kind: 'session' };
    if (!res.ok) {
      if ((await codeOf(res)) === 'power_unavailable') return { ok: false, kind: 'unavailable' };
      return { ok: false, kind: res.status >= 500 ? 'network' : 'shape' };
    }
    const body = (await res.json()) as unknown;
    return isPowerState(body) ? { ok: true, state: body } : { ok: false, kind: 'shape' };
  } catch {
    return { ok: false, kind: 'shape' };
  }
}

/**
 * The client waits this long for POST /api/v1/power: the service checks the
 * password (about 0.5 s), reads the update state (at most 3 s) and waits for
 * the broker (at most 20 s), all inside the server's write timeout of 30 s.
 */
export const powerRequestTimeoutMs = 28000;

/** POST /api/v1/power. The password is sent in this one request and kept nowhere. */
export async function requestPower(action: PowerAction, currentPassword: string, fetchFn: typeof fetch = fetch): Promise<PowerRequestResult> {
  const r = await postJSON('/api/v1/power', { action, current_password: currentPassword }, fetchFn, powerRequestTimeoutMs);
  if (!r.ok) {
    if (r.code === 'network') return { ok: false, kind: 'network' };
    if (r.code === 'unauthenticated' || r.code === 'setup_required') return { ok: false, kind: 'session' };
    if (r.code === 'power_unavailable') return { ok: false, kind: 'unavailable' };
    return { ok: false, kind: 'error', code: r.code, retryAfter: r.retryAfter };
  }
  return r.status === 200 && isObj(r.body) && r.body.schema_version === 1 && r.body.accepted === true ? { ok: true } : { ok: false, kind: 'shape' };
}

/** One poll of GET /healthz (public, no session): true when the service answered. */
export async function probeHealth(fetchFn: typeof fetch = fetch, timeoutMs = 4000): Promise<boolean> {
  try {
    const res = await fetchFn('/healthz', { cache: 'no-store', credentials: 'omit', signal: AbortSignal.timeout(timeoutMs) });
    return res.ok;
  } catch {
    return false;
  }
}

// ---- the accepted signal -----------------------------------------------------------

// After accepted the app shows the status view instead of the shell (App.svelte),
// so no page polls, navigates or sends anything else. The value is the action
// only; nothing else is kept.
type Listener = (action: PowerAction) => void;
const listeners = new Set<Listener>();

/** App.svelte listens; the returned function stops listening. */
export function onPowerAccepted(listener: Listener): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

/** The power card reports an accepted request. */
export function powerAccepted(action: PowerAction): void {
  for (const l of [...listeners]) l(action);
}
