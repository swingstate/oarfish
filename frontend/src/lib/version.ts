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
