/** A version as the service reports it: letters, digits and . _ + - only, so nothing else can reach the page. */
const plausible = /^[A-Za-z0-9._+-]{1,64}$/;

/**
 * Reads the Oarfish version from the public /healthz route. Returns null when
 * anything goes wrong: the version line is a convenience and shows nothing
 * rather than an error.
 */
export async function fetchVersion(fetchFn: typeof fetch = fetch, timeoutMs = 5000): Promise<string | null> {
  try {
    const res = await fetchFn('/healthz', { cache: 'no-store', credentials: 'same-origin', signal: AbortSignal.timeout(timeoutMs) });
    if (!res.ok) return null;
    const body = (await res.json()) as { version?: unknown } | null;
    return typeof body?.version === 'string' && plausible.test(body.version) ? body.version : null;
  } catch {
    return null;
  }
}

// ---- the version of this page (task N0, ADR 0011 A8.8) --------------------------
//
// After a GUI update the service runs the new version, but the browser still
// shows the page it loaded before: the old code stays in memory, and nothing
// tells it to load again. The page therefore remembers the version that served
// it (the first /healthz answer after it loaded) and loads itself again once
// /healthz reports another version.

let ownVersion: Promise<string | null> | null = null;

/**
 * The version of the service that served this page. The first call reads
 * /healthz; every later call returns that answer, also when it was null.
 * main.ts calls it as the page starts.
 */
export function pageVersion(fetchFn: typeof fetch = fetch): Promise<string | null> {
  ownVersion ??= fetchVersion(fetchFn);
  return ownVersion;
}

/** For tests: forget the remembered version. */
export function forgetPageVersion(): void {
  ownVersion = null;
}

/** The reload, replaceable in tests (jsdom cannot reload). */
export const pageReload = { run: (): void => location.reload() };

const reloadKey = 'oarfish.reloadedFor';

export type ReloadDecision = { kind: 'same' | 'unknown' | 'reload' | 'ask'; version: string | null };

/**
 * Compares the version of this page with the one /healthz reports now and
 * reloads the page when they differ. The kind is 'reload' (the reload has been started),
 * 'same', 'unknown' (one of the two could not be read: nothing happens), or
 * 'ask' (the page was already reloaded once for this version and still differs,
 * or the browser keeps no session storage: the person is asked to reload,
 * so that a cached page can never cause a reload loop).
 */
export async function reloadIfNewVersion(fetchFn: typeof fetch = fetch): Promise<ReloadDecision> {
  const [mine, now] = await Promise.all([pageVersion(fetchFn), fetchVersion(fetchFn)]);
  if (mine === null || now === null) return { kind: 'unknown', version: now };
  if (mine === now) return { kind: 'same', version: now };
  try {
    if (sessionStorage.getItem(reloadKey) === now) return { kind: 'ask', version: now };
    sessionStorage.setItem(reloadKey, now);
  } catch {
    return { kind: 'ask', version: now };
  }
  pageReload.run();
  return { kind: 'reload', version: now };
}
