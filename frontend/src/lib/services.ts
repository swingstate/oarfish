// Client for the services routes of the Activity page (ADR 0011 B4, step G7.4;
// docs/UI.md, "Activity page"). Every answer is checked at run time: a shape
// this version does not expect is a load error, never a half-shown page. A word
// of a closed set that this version does not know (a state, a start mode, a
// kind) is kept and shown neutrally. Names, descriptions and codes are
// untrusted text: the page shows them as text nodes only. Nothing here throws.
import { handleUnauthorized, postJSON } from './session';

/** The ActiveState words of the service (ADR 0011 A4.3). */
export const activeStates = ['active', 'reloading', 'inactive', 'failed', 'activating', 'deactivating', 'maintenance', 'refreshing'] as const;

/** The start modes: the unit-file states of list-unit-files (ADR 0011 A3.2), without alias, which is never a row. */
export const startModes = ['enabled', 'enabled-runtime', 'disabled', 'static', 'masked', 'indirect', 'linked', 'generated', 'transient'] as const;

/** The kinds of critical services of ADR 0011 B3 rule 11 (A3.4). */
export const criticalKinds = ['network', 'ssh', 'remote_access', 'container', 'time'] as const;

/** The three actions. An action word this version does not know is not offered. */
export const serviceActions = ['start', 'stop', 'restart'] as const;
export type ServiceAction = (typeof serviceActions)[number];

export interface ServiceRow {
  name: string;
  /** Cleaned plain text from systemd (at most 200 characters); untrusted. */
  description: string;
  load_state: string;
  active_state: string;
  sub_state: string;
  /** A unit-file state, or '' when the service could not tell. */
  start_mode: string;
  denied: boolean;
  restart_only: boolean;
  /** A kind of rule 11, or ''. */
  critical: string;
  actions: string[];
}

/**
 * The group of a row's active state for the count line and the state filter.
 * The service counts only active, failed and inactive (internal/services,
 * count); every other state of the closed set (reloading, activating,
 * deactivating, maintenance, refreshing) is in transition.
 */
export type StateGroup = 'active' | 'failed' | 'inactive' | 'transition';

export function stateGroup(activeState: string): StateGroup {
  return activeState === 'active' || activeState === 'failed' || activeState === 'inactive' ? activeState : 'transition';
}

/** The rows in transition: in the total, in none of the three counts. */
export function transitionCount(c: ServiceCounts): number {
  return Math.max(0, c.total - c.active - c.failed - c.inactive);
}

export interface ServiceCounts {
  total: number;
  active: number;
  failed: number;
  inactive: number;
}

/** GET /api/v1/services (schema_version 1). */
export interface ServicesList {
  schema_version: 1;
  read_at: string;
  broker_available: boolean;
  counts: ServiceCounts;
  services: ServiceRow[];
}

/** The steps of the last action: a closed set of the service. */
export const actionSteps = ['running', 'done'] as const;

export interface ActionView {
  unit: string;
  action: string;
  step: (typeof actionSteps)[number];
  started_at: string;
  finished_at?: string;
  /** A broker code or unavailable; only when done. */
  code?: string;
  state?: string;
  retry_after_s?: number;
}

/** GET /api/v1/services/action (schema_version 1). */
export interface ActionState {
  schema_version: 1;
  action: ActionView | null;
}

// ---- shape checks ------------------------------------------------------------

type Obj = Record<string, unknown>;

const isObj = (v: unknown): v is Obj => typeof v === 'object' && v !== null && !Array.isArray(v);
const isCount = (v: unknown): v is number => typeof v === 'number' && Number.isInteger(v) && v >= 0;
const optString = (o: Obj, k: string) => o[k] === undefined || typeof o[k] === 'string';
const isStrings = (v: unknown): v is string[] => Array.isArray(v) && v.every((d) => typeof d === 'string');

function isRow(v: unknown): v is ServiceRow {
  return (
    isObj(v) &&
    typeof v.name === 'string' &&
    v.name !== '' &&
    typeof v.description === 'string' &&
    typeof v.load_state === 'string' &&
    typeof v.active_state === 'string' &&
    typeof v.sub_state === 'string' &&
    typeof v.start_mode === 'string' &&
    typeof v.denied === 'boolean' &&
    typeof v.restart_only === 'boolean' &&
    typeof v.critical === 'string' &&
    isStrings(v.actions)
  );
}

function isCounts(v: unknown): v is ServiceCounts {
  return isObj(v) && isCount(v.total) && isCount(v.active) && isCount(v.failed) && isCount(v.inactive);
}

/** Whether a value is a service list of schema version 1. */
export function isServicesList(v: unknown): v is ServicesList {
  return (
    isObj(v) &&
    v.schema_version === 1 &&
    typeof v.read_at === 'string' &&
    typeof v.broker_available === 'boolean' &&
    isCounts(v.counts) &&
    Array.isArray(v.services) &&
    v.services.every(isRow)
  );
}

function isActionView(v: unknown): v is ActionView {
  return (
    isObj(v) &&
    typeof v.unit === 'string' &&
    typeof v.action === 'string' &&
    (actionSteps as readonly unknown[]).includes(v.step) &&
    typeof v.started_at === 'string' &&
    optString(v, 'finished_at') &&
    optString(v, 'code') &&
    optString(v, 'state') &&
    (v.retry_after_s === undefined || isCount(v.retry_after_s))
  );
}

/** Whether a value is the last-action answer of schema version 1. */
export function isActionState(v: unknown): v is ActionState {
  return isObj(v) && v.schema_version === 1 && (v.action === null || isActionView(v.action));
}

// ---- requests ------------------------------------------------------------------

/**
 * The result of a request. `session`: the session is gone and the app has been
 * told (login page). `systemd`: this system has no service management
 * (services_unavailable). `network`: no answer or the service could not read the
 * list. `shape`: an answer this version cannot read.
 */
export type ServicesFailure =
  | { ok: false; kind: 'session' }
  | { ok: false; kind: 'systemd' }
  | { ok: false; kind: 'network' }
  | { ok: false; kind: 'shape' }
  | { ok: false; kind: 'error'; code: string; retryAfter: number | null };

export type ListResult = { ok: true; list: ServicesList } | ServicesFailure;
export type ActionStateResult = { ok: true; state: ActionState } | ServicesFailure;
export type SubmitResult = { ok: true } | ServicesFailure;

async function codeOf(res: Response): Promise<string> {
  try {
    const body = (await res.json()) as { error?: { code?: unknown } };
    return typeof body.error?.code === 'string' ? body.error.code : '';
  } catch {
    return '';
  }
}

async function getJSON(url: string, fetchFn: typeof fetch, timeoutMs: number): Promise<{ ok: true; body: unknown } | ServicesFailure> {
  let res: Response;
  try {
    res = await fetchFn(url, { cache: 'no-store', credentials: 'same-origin', signal: AbortSignal.timeout(timeoutMs) });
  } catch {
    return { ok: false, kind: 'network' };
  }
  try {
    if (await handleUnauthorized(res)) return { ok: false, kind: 'session' };
    if (!res.ok) {
      const code = await codeOf(res);
      if (code === 'services_unavailable') return { ok: false, kind: 'systemd' };
      return { ok: false, kind: res.status >= 500 ? 'network' : 'shape' };
    }
    return { ok: true, body: (await res.json()) as unknown };
  } catch {
    return { ok: false, kind: 'shape' };
  }
}

/** GET /api/v1/services. One list read takes two systemctl calls of at most 5 s each. */
export async function fetchServices(fetchFn: typeof fetch = fetch, timeoutMs = 15000): Promise<ListResult> {
  const r = await getJSON('/api/v1/services', fetchFn, timeoutMs);
  if (!r.ok) return r;
  return isServicesList(r.body) ? { ok: true, list: r.body } : { ok: false, kind: 'shape' };
}

/** GET /api/v1/services/action: cheap, from the service's memory. */
export async function fetchAction(fetchFn: typeof fetch = fetch, timeoutMs = 10000): Promise<ActionStateResult> {
  const r = await getJSON('/api/v1/services/action', fetchFn, timeoutMs);
  if (!r.ok) return r;
  return isActionState(r.body) ? { ok: true, state: r.body } : { ok: false, kind: 'shape' };
}

/**
 * POST /api/v1/services/action. The service answers 202 and runs the action as
 * a job; the page then reads the last action every 2 s until it is done.
 */
export async function submitAction(unit: string, action: ServiceAction, fetchFn: typeof fetch = fetch): Promise<SubmitResult> {
  const r = await postJSON('/api/v1/services/action', { unit, action }, fetchFn);
  if (!r.ok) {
    if (r.code === 'network') return { ok: false, kind: 'network' };
    if (r.code === 'unauthenticated' || r.code === 'setup_required') return { ok: false, kind: 'session' };
    if (r.code === 'services_unavailable') return { ok: false, kind: 'systemd' };
    return { ok: false, kind: 'error', code: r.code, retryAfter: r.retryAfter };
  }
  return r.status === 202 && isObj(r.body) && r.body.schema_version === 1 && r.body.accepted === true ? { ok: true } : { ok: false, kind: 'shape' };
}
