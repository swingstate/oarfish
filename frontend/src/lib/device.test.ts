import { describe, expect, it, vi } from 'vitest';
import { fetchDevice, formatCelsius, formatMemory } from './device';
import { piZero2W } from './device.fixtures';

const json = (body: unknown, init?: ResponseInit) =>
  new Response(JSON.stringify(body), { headers: { 'Content-Type': 'application/json' }, ...init });

describe('fetchDevice', () => {
  it('returns the device for a valid response', async () => {
    const fetchFn = vi.fn().mockResolvedValue(json(piZero2W));
    const result = await fetchDevice(fetchFn);
    expect(result).toEqual({ ok: true, device: piZero2W });
    expect(fetchFn).toHaveBeenCalledWith('/api/v1/device', expect.objectContaining({ cache: 'no-store' }));
  });

  it('fails for a non-2xx response', async () => {
    expect(await fetchDevice(vi.fn().mockResolvedValue(json({}, { status: 503 })))).toEqual({ ok: false });
  });

  it('fails for an unexpected body or schema version', async () => {
    expect(await fetchDevice(vi.fn().mockResolvedValue(json({ hello: 'world' })))).toEqual({ ok: false });
    expect(await fetchDevice(vi.fn().mockResolvedValue(json({ ...piZero2W, schema_version: 2 })))).toEqual({
      ok: false,
    });
    expect(await fetchDevice(vi.fn().mockResolvedValue(json(null)))).toEqual({ ok: false });
  });

  it('fails when the request fails', async () => {
    expect(await fetchDevice(vi.fn().mockRejectedValue(new TypeError('offline')))).toEqual({ ok: false });
  });
});

describe('formatting', () => {
  it('formats memory in MiB and keeps unknown values null', () => {
    expect(formatMemory(415404 * 1024)).toBe('406 MiB');
    expect(formatMemory(null)).toBeNull();
  });

  it('formats a temperature with a German decimal comma', () => {
    expect(formatCelsius(43.3)).toBe('43,3 °C');
    expect(formatCelsius(null)).toBeNull();
  });
});
