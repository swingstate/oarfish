export type HealthState = 'checking' | 'ok' | 'unreachable';

/** Asks the Oarfish core whether it is alive. Never throws. */
export async function fetchHealth(
  fetchFn: typeof fetch = fetch,
  timeoutMs = 5000,
): Promise<Exclude<HealthState, 'checking'>> {
  try {
    const res = await fetchFn('/healthz', {
      cache: 'no-store',
      signal: AbortSignal.timeout(timeoutMs),
    });
    if (!res.ok) return 'unreachable';
    const body: unknown = await res.json();
    const status = (body as { status?: unknown } | null)?.status;
    return status === 'ok' ? 'ok' : 'unreachable';
  } catch {
    return 'unreachable';
  }
}
