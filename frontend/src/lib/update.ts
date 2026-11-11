// Client for the update routes of the service (ADR 0010 U9, A11; docs/UI.md,
// "Update page"). Every answer is checked at run time: a shape this version does
// not expect is a load error, never a half-shown page. Nothing here throws.
//
// The page never calls the notice route of A11.1 (D-C68); a test checks that the
// frontend sources do not name it. The password goes from the input into the
// request body and nowhere else: this module keeps no copy.
import { handleUnauthorized, postJSON } from './session';

/** An error as the State shows it. The message is the server's English text and is never shown. */
export interface UpdateErrorView {
  code: string;
  message: string;
  retry_after_seconds?: number;
}

export interface HelperView {
  present: boolean;
  error?: UpdateErrorView;
  /** A phase of the helper's state machine. A phase this version does not know is kept and shown neutrally. */
  phase?: string;
  staged_version?: string;
  needs_attention?: boolean;
  cooldown_version?: string;
  cooldown_remaining_s?: number;
  /** A component update changed the helper's socket unit; it takes effect at the next boot (ADR 0010 A15 D4). */
  socket_restart_pending?: boolean;
  /** The helper cannot be reached because it hands over to its new version (ADR 0010 A17, F4); only with present false. */
  handing_over?: boolean;
}

export type CheckState = 'never' | 'ok' | 'error';

export interface CheckView {
  state: CheckState;
  checked_at?: string;
  error?: UpdateErrorView;
  throttled?: boolean;
  next_check_at?: string;
}

export interface Available {
  version: string;
  channel: 'stable' | 'prerelease';
  /** From GitHub, not signed: shown as plain text only. */
  notes: string;
  download_size: number;
  verified: boolean;
  verify_error?: UpdateErrorView;
}

/** The steps of a job: a closed set of the service (backend/internal/updater/apply.go). */
export const jobSteps = [
  'checking',
  'fetching_metadata',
  'downloading',
  'staging',
  'awaiting_components_confirmation',
  'activating',
  'done',
  'failed',
] as const;
export type JobStep = (typeof jobSteps)[number];

export interface JobView {
  kind: 'check' | 'apply';
  step: JobStep;
  version?: string;
  running: boolean;
  started_at: string;
  finished_at?: string;
  error?: UpdateErrorView;
  /** What the staged release replaces besides the release itself: names of the closed list of ADR 0010 U2. */
  differs?: string[];
}

/** The outcomes of the helper (ADR 0010 U8). An outcome this version does not know is kept and shown neutrally. */
export const outcomes = ['success', 'rolled_back', 'components_rolled_back', 'rollback_failed', 'aborted'] as const;

export interface ResultView {
  to: string;
  from: string;
  outcome: string;
  reason_code?: string;
  finished_at?: string;
  /** The transaction replaced the helper components, whatever the outcome of its release (ADR 0010 U13 F31). */
  components_updated?: boolean;
}

/** The names of ADR 0010 U2 that a release can replace besides itself (a closed list of the service). */
export const componentNames = [
  'oarfish-update-helper',
  'oarfish-update-helper.socket',
  'oarfish-update-helper.service',
  'oarfish-update-recover.service',
  'oarfish.service',
] as const;

/** The codes for which the device needs the installer (ADR 0010 A16 E1, A17 F1). */
export const installerCodes = ['installer_required', 'key_changed', 'unit_rejected', 'requirements_not_met'] as const;

export interface InstallerView {
  required: boolean;
  version: string;
  /** Why the installer is needed: one of installerCodes. A service before step c of G6.10 sends none. */
  code?: string;
  /** The service's English instruction. The page shows its own fixed text instead. */
  instruction: string;
  differs: string[];
}

/** GET /api/v1/update (schema_version 1). */
export interface UpdateState {
  schema_version: 1;
  installed_version: string;
  helper: HelperView;
  check: CheckView;
  available: Available | null;
  job: JobView | null;
  last_result: ResultView | null;
  installer: InstallerView | null;
}

// ---- shape checks ------------------------------------------------------------

type Obj = Record<string, unknown>;

const isObj = (v: unknown): v is Obj => typeof v === 'object' && v !== null && !Array.isArray(v);
const optString = (o: Obj, k: string) => o[k] === undefined || typeof o[k] === 'string';
const optBool = (o: Obj, k: string) => o[k] === undefined || typeof o[k] === 'boolean';
const optCount = (o: Obj, k: string) => o[k] === undefined || isCount(o[k]);
const isStrings = (v: unknown): v is string[] => Array.isArray(v) && v.every((d) => typeof d === 'string');
const optStrings = (o: Obj, k: string) => o[k] === undefined || isStrings(o[k]);
const isCount = (v: unknown): v is number => typeof v === 'number' && Number.isFinite(v) && v >= 0;

function isError(v: unknown): v is UpdateErrorView {
  return isObj(v) && typeof v.code === 'string' && typeof v.message === 'string' && optCount(v, 'retry_after_seconds');
}

const optError = (o: Obj, k: string) => o[k] === undefined || isError(o[k]);

function isHelper(v: unknown): v is HelperView {
  return (
    isObj(v) &&
    typeof v.present === 'boolean' &&
    optError(v, 'error') &&
    optString(v, 'phase') &&
    optString(v, 'staged_version') &&
    optBool(v, 'needs_attention') &&
    optString(v, 'cooldown_version') &&
    optCount(v, 'cooldown_remaining_s') &&
    optBool(v, 'socket_restart_pending') &&
    optBool(v, 'handing_over')
  );
}

function isCheck(v: unknown): v is CheckView {
  return (
    isObj(v) &&
    (v.state === 'never' || v.state === 'ok' || v.state === 'error') &&
    optString(v, 'checked_at') &&
    optError(v, 'error') &&
    optBool(v, 'throttled') &&
    optString(v, 'next_check_at')
  );
}

function isAvailable(v: unknown): v is Available {
  return (
    isObj(v) &&
    typeof v.version === 'string' &&
    (v.channel === 'stable' || v.channel === 'prerelease') &&
    typeof v.notes === 'string' &&
    isCount(v.download_size) &&
    typeof v.verified === 'boolean' &&
    optError(v, 'verify_error')
  );
}

function isJob(v: unknown): v is JobView {
  return (
    isObj(v) &&
    (v.kind === 'check' || v.kind === 'apply') &&
    (jobSteps as readonly unknown[]).includes(v.step) &&
    optString(v, 'version') &&
    typeof v.running === 'boolean' &&
    typeof v.started_at === 'string' &&
    optString(v, 'finished_at') &&
    optError(v, 'error') &&
    optStrings(v, 'differs')
  );
}

function isResult(v: unknown): v is ResultView {
  return (
    isObj(v) &&
    typeof v.to === 'string' &&
    typeof v.from === 'string' &&
    typeof v.outcome === 'string' &&
    optString(v, 'reason_code') &&
    optString(v, 'finished_at') &&
    optBool(v, 'components_updated')
  );
}

function isInstaller(v: unknown): v is InstallerView {
  return (
    isObj(v) &&
    typeof v.required === 'boolean' &&
    typeof v.version === 'string' &&
    optString(v, 'code') &&
    typeof v.instruction === 'string' &&
    isStrings(v.differs)
  );
}

/** Whether a value is a State of schema version 1. */
export function isUpdateState(v: unknown): v is UpdateState {
  return (
    isObj(v) &&
    v.schema_version === 1 &&
    typeof v.installed_version === 'string' &&
    isHelper(v.helper) &&
    isCheck(v.check) &&
    (v.available === null || isAvailable(v.available)) &&
    (v.job === null || isJob(v.job)) &&
    (v.last_result === null || isResult(v.last_result)) &&
    (v.installer === null || isInstaller(v.installer))
  );
}

// ---- requests ------------------------------------------------------------------

/**
 * The result of a request. `session` means the session is gone: the app has
 * already been told (login page). `network` means no answer at all, `shape` an
 * answer this version cannot read.
 */
export type UpdateFailure =
  | { ok: false; kind: 'session' }
  | { ok: false; kind: 'network' }
  | { ok: false; kind: 'shape' }
  | { ok: false; kind: 'error'; code: string; retryAfter: number | null };

export type StateResult = { ok: true; state: UpdateState } | UpdateFailure;
export type ActionResult = { ok: true } | UpdateFailure;

/** GET /api/v1/update. The service asks the helper for up to 10 s, so the timeout is longer than that. */
export async function fetchUpdateState(fetchFn: typeof fetch = fetch, timeoutMs = 15000): Promise<StateResult> {
  let res: Response;
  try {
    res = await fetchFn('/api/v1/update', { cache: 'no-store', credentials: 'same-origin', signal: AbortSignal.timeout(timeoutMs) });
  } catch {
    return { ok: false, kind: 'network' };
  }
  try {
    if (await handleUnauthorized(res)) return { ok: false, kind: 'session' };
    if (!res.ok) return { ok: false, kind: res.status >= 500 ? 'network' : 'shape' };
    const body: unknown = await res.json();
    return isUpdateState(body) ? { ok: true, state: body } : { ok: false, kind: 'shape' };
  } catch {
    return { ok: false, kind: 'shape' };
  }
}

function failure(r: { code: string; retryAfter: number | null }): UpdateFailure {
  if (r.code === 'network') return { ok: false, kind: 'network' };
  if (r.code === 'unauthenticated' || r.code === 'setup_required') return { ok: false, kind: 'session' };
  return { ok: false, kind: 'error', code: r.code, retryAfter: r.retryAfter };
}

/** POST /api/v1/update/check. Without force it is the check of opening the page, with force "Check now". */
export async function checkForUpdate(force: boolean, fetchFn: typeof fetch = fetch): Promise<StateResult> {
  const r = await postJSON('/api/v1/update/check', force ? { force: true } : {}, fetchFn);
  if (!r.ok) return failure(r);
  return isUpdateState(r.body) ? { ok: true, state: r.body } : { ok: false, kind: 'shape' };
}

const accepted = (v: unknown) => isObj(v) && v.schema_version === 1 && v.accepted === true;

/**
 * POST /api/v1/update/apply. The service answers 202 and runs the job; the page
 * polls the State. `componentsConfirmed` is the second confirmation (ADR 0010
 * U15 rule 7), sent only from the screen of the step awaiting_components_confirmation.
 */
export async function applyUpdate(version: string, currentPassword: string, componentsConfirmed = false, fetchFn: typeof fetch = fetch): Promise<ActionResult> {
  const body: Record<string, unknown> = { version, current_password: currentPassword };
  if (componentsConfirmed) body.components_confirmed = true;
  const r = await postJSON('/api/v1/update/apply', body, fetchFn);
  if (!r.ok) return failure(r);
  return r.status === 202 && accepted(r.body) ? { ok: true } : { ok: false, kind: 'shape' };
}

/** POST /api/v1/update/discard: removes a staged release. */
export async function discardUpdate(version: string, fetchFn: typeof fetch = fetch): Promise<ActionResult> {
  const r = await postJSON('/api/v1/update/discard', { version }, fetchFn);
  if (!r.ok) return failure(r);
  return isObj(r.body) && r.body.schema_version === 1 && r.body.ok === true ? { ok: true } : { ok: false, kind: 'shape' };
}
