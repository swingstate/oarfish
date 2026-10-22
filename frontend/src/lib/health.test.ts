import { describe, expect, it, vi } from 'vitest';
import { fetchHealth } from './health';

const json = (body: unknown, init?: ResponseInit) =>
  new Response(JSON.stringify(body), { headers: { 'Content-Type': 'application/json' }, ...init });

describe('fetchHealth', () => {
  it('reports ok for {"status":"ok"}', async () => {
    const fetchFn = vi.fn().mockResolvedValue(json({ status: 'ok' }));
    expect(await fetchHealth(fetchFn)).toBe('ok');
    expect(fetchFn).toHaveBeenCalledWith('/healthz', expect.objectContaining({ cache: 'no-store' }));
  });

  it('reports unreachable for a non-2xx response', async () => {
    const fetchFn = vi.fn().mockResolvedValue(json({}, { status: 503 }));
    expect(await fetchHealth(fetchFn)).toBe('unreachable');
  });

  it('reports unreachable for an unexpected body', async () => {
    const fetchFn = vi.fn().mockResolvedValue(json({ status: 'degraded' }));
    expect(await fetchHealth(fetchFn)).toBe('unreachable');
  });

  it('reports unreachable when the request fails', async () => {
    const fetchFn = vi.fn().mockRejectedValue(new TypeError('network down'));
    expect(await fetchHealth(fetchFn)).toBe('unreachable');
  });
});
