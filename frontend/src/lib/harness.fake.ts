// Shared setup for tests that render the Home page against a fake server.
import { screen } from '@testing-library/svelte';
import { vi } from 'vitest';
import { FakeEventSource } from './eventsource.fake';
import { piMetrics, piSnapshot } from './metrics.fixtures';
import type { MetricsResponse, Snapshot } from './metrics';

export function installFakes(): void {
  FakeEventSource.reset();
  vi.stubGlobal('EventSource', FakeEventSource);
}

export function stubMetrics(body: MetricsResponse | null): void {
  vi.stubGlobal(
    'fetch',
    body === null
      ? vi.fn().mockRejectedValue(new TypeError('offline'))
      : vi.fn().mockImplementation(async () => new Response(JSON.stringify(body))),
  );
}

/** Renders a component that shows Home, lets the one-shot request answer, and opens the stream. */
export async function openHome(render: () => unknown, body: MetricsResponse = piMetrics): Promise<FakeEventSource> {
  stubMetrics(body);
  render();
  await screen.findByTestId('home');
  FakeEventSource.last.open();
  return FakeEventSource.last;
}

/** A metrics response built from the Pi fixture with changes applied to a copy. */
export function metricsWith(change: (m: MetricsResponse) => void): MetricsResponse {
  const m = structuredClone(piMetrics);
  change(m);
  return m;
}

export const at = (seconds: number): string => new Date(Date.parse(piSnapshot.collected_at) + seconds * 1000).toISOString();

export function snapshotAt(seconds: number, change: (s: Snapshot) => void = () => {}): Snapshot {
  const s = structuredClone(piSnapshot);
  s.collected_at = at(seconds);
  change(s);
  return s;
}

export const textOf = (testId: string): string | undefined => screen.getByTestId(testId).textContent?.replace(/\s+/g, ' ').trim();
