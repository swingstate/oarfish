// Client for the access-control endpoints (docs/AUTH.md, ADR 0009).
//
// The CSRF token lives in this module's variable and nowhere else: not in
// localStorage, sessionStorage, a cookie, the address or a log. The session
// cookie itself is HttpOnly; the page never sees it. Every state-changing
// request sends the token in X-Oarfish-CSRF and its body as JSON.

export type SessionState = 'setup_required' | 'login_required' | 'authenticated';

/** GET /api/v1/session (schema_version 1). The other fields exist only when authenticated. */
export interface SessionInfo {
  schema_version: number;
  state: SessionState;
  username?: string;
  csrf_token?: string;
  expires_at?: string;
  idle_expires_at?: string;
  idle_timeout_seconds?: number;
}

let csrf: string | null = null;
// The signed-in user's name, for the user menu and the account page. Not a secret.
let user: string | null = null;

/** The name of the signed-in administrator, or null. */
export function sessionUser(): string | null {
  return user;
}

/** Why the app has to leave the signed-in pages. */
export type SessionLoss = 'expired' | 'setup_required';

let lossListener: ((reason: SessionLoss) => void) | null = null;

/** The app registers the one listener that returns to the login or setup page. */
export function onSessionLost(listener: (reason: SessionLoss) => void): () => void {
  lossListener = listener;
  return () => {
    if (lossListener === listener) lossListener = null;
  };
}

/** Forgets the CSRF token and tells the app that the session is gone. */
export function sessionLost(reason: SessionLoss): void {
  csrf = null;
  user = null;
  lossListener?.(reason);
}

/** For tests: whether a CSRF token is held. */
export function hasCsrfToken(): boolean {
  return csrf !== null;
}

/** Forgets the CSRF token (logout). */
export function forgetSession(): void {
  csrf = null;
  user = null;
}

function isSessionInfo(v: unknown): v is SessionInfo {
  const s = v as Partial<SessionInfo> | null;
  return (
    typeof s === 'object' &&
    s !== null &&
    s.schema_version === 1 &&
    (s.state === 'setup_required' || s.state === 'login_required' || s.state === 'authenticated') &&
    (s.state !== 'authenticated' || (typeof s.username === 'string' && typeof s.csrf_token === 'string'))
  );
}

function adopt(s: SessionInfo): SessionInfo {
  const signedIn = s.state === 'authenticated';
  csrf = signedIn ? (s.csrf_token ?? null) : null;
  user = signedIn ? (s.username ?? null) : null;
  return s;
}

export type SessionResult = { ok: true; session: SessionInfo } | { ok: false };

/** Asks the server for the session state. Never throws. It does not extend the session. */
export async function fetchSession(fetchFn: typeof fetch = fetch, timeoutMs = 5000): Promise<SessionResult> {
  try {
    const res = await fetchFn('/api/v1/session', { cache: 'no-store', credentials: 'same-origin', signal: AbortSignal.timeout(timeoutMs) });
    if (!res.ok) return { ok: false };
    const body: unknown = await res.json();
    if (!isSessionInfo(body)) return { ok: false };
    return { ok: true, session: adopt(body) };
  } catch {
    return { ok: false };
  }
}

/** Reads the typed error code of an API answer, or '' when there is none. */
async function errorOf(res: Response): Promise<{ code: string; retryAfter: number | null }> {
  let code = '';
  let retryAfter: number | null = null;
  try {
    const body = (await res.json()) as { error?: { code?: unknown; retry_after_seconds?: unknown } };
    if (typeof body.error?.code === 'string') code = body.error.code;
    if (typeof body.error?.retry_after_seconds === 'number') retryAfter = body.error.retry_after_seconds;
  } catch {
    // no JSON body
  }
  if (retryAfter === null) {
    const header = Number(res.headers.get('Retry-After'));
    if (Number.isFinite(header) && header > 0) retryAfter = header;
  }
  return { code, retryAfter };
}

/**
 * Called by the read clients (device, metrics) for every answer. A 401 that
 * says the session is gone sends the app back to the login or setup page; it
 * returns true then.
 */
export async function handleUnauthorized(res: Response): Promise<boolean> {
  if (res.status !== 401) return false;
  const { code } = await errorOf(res);
  sessionLost(code === 'setup_required' ? 'setup_required' : 'expired');
  return true;
}

export type ActionResult =
  | { ok: true; session: SessionInfo | null }
  | { ok: false; code: string; retryAfter: number | null };

async function send(method: 'POST' | 'DELETE', url: string, body: unknown, fetchFn: typeof fetch): Promise<ActionResult> {
  const headers: Record<string, string> = { 'Content-Type': 'application/json' };
  if (csrf !== null) headers['X-Oarfish-CSRF'] = csrf;
  let res: Response;
  try {
    res = await fetchFn(url, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
      cache: 'no-store',
      credentials: 'same-origin',
      signal: AbortSignal.timeout(20000),
    });
  } catch {
    return { ok: false, code: 'network', retryAfter: null };
  }
  if (!res.ok) {
    const err = await errorOf(res);
    // Only these two mean the session is gone; a wrong password is an answer to show.
    if (res.status === 401 && (err.code === 'unauthenticated' || err.code === 'setup_required')) {
      sessionLost(err.code === 'setup_required' ? 'setup_required' : 'expired');
    }
    return { ok: false, code: err.code || 'unknown', retryAfter: err.retryAfter };
  }
  try {
    const answer: unknown = await res.json();
    return { ok: true, session: isSessionInfo(answer) ? adopt(answer) : null };
  } catch {
    return { ok: true, session: null };
  }
}

export function login(username: string, password: string, fetchFn: typeof fetch = fetch): Promise<ActionResult> {
  return send('POST', '/api/v1/session', { username, password }, fetchFn);
}

export function setup(setupToken: string, username: string, password: string, fetchFn: typeof fetch = fetch): Promise<ActionResult> {
  return send('POST', '/api/v1/setup', { setup_token: setupToken, username, password }, fetchFn);
}

export async function logout(fetchFn: typeof fetch = fetch): Promise<ActionResult> {
  const result = await send('DELETE', '/api/v1/session', undefined, fetchFn);
  forgetSession();
  return result;
}

export function changePassword(currentPassword: string, newPassword: string, fetchFn: typeof fetch = fetch): Promise<ActionResult> {
  return send('POST', '/api/v1/account/password', { current_password: currentPassword, new_password: newPassword }, fetchFn);
}

// ---- local checks before sending (the server checks again) ---------------

/** Unicode code points, as the server counts them. */
export const codePoints = (s: string): number => [...s].length;

export const usernamePattern = /^[a-z0-9._-]{1,32}$/;

/** The error code the server would give for a new password, or null. Length only; the list check is the server's. */
export function passwordProblem(password: string): string | null {
  const n = codePoints(password);
  if (n < 12) return 'password_too_short';
  if (n > 128) return 'password_too_long';
  return null;
}
