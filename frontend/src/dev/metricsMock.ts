// Development scenario mode for the Dashboard's network card and the Gerät
// page (task N0, docs/DEVELOPMENT.md).
//
// Only in the development server (import.meta.env.DEV) and only when the address
// has ?mock=<scenario>: a fake fetch layer answers the session route,
// /api/v1/metrics and /api/v1/device from the fixtures, and a fake EventSource
// sends a metrics event every 2 seconds, so the default routes, the DNS servers
// and the memory text can be seen without a device. Every other request passes
// through unchanged. The production build does not contain this file: main.ts
// imports it in a branch that Vite removes, and the build fails if the marker
// below appears in the output (vite.config.ts). Synthetic data only.
import { genericArm64, piZero2W } from '../lib/device.fixtures';
import type { Device } from '../lib/device';
import { hiddenRouting, noRouteRouting, piMetrics, piRouting, stubRouting, unreadableRouting } from '../lib/metrics.fixtures';
import type { MetricsResponse, Routing } from '../lib/metrics';

/** A unique string: the production build is checked for it. */
export const devMockMarker = 'oarfish-dev-mock';

interface Scenario {
  routing: Routing;
  device: Device;
}

/** The scenarios of docs/DEVELOPMENT.md, "Network card and Gerät page". */
const scenarios: Record<string, () => Scenario> = {
  'network-route-dns': () => ({ routing: piRouting, device: piZero2W }),
  'network-no-route': () => ({ routing: noRouteRouting, device: piZero2W }),
  'network-dns-stub': () => ({ routing: stubRouting, device: piZero2W }),
  'network-hidden': () => ({ routing: hiddenRouting, device: piZero2W }),
  'network-unreadable': () => ({ routing: unreadableRouting, device: piZero2W }),
  'device-memory-unknown': () => ({ routing: piRouting, device: genericArm64 }),
};

/** The names, for the console hint and the documentation. */
export function metricsScenarioNames(): string[] {
  return Object.keys(scenarios);
}

const json = (body: unknown) => new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } });
const session = { schema_version: 1, state: 'authenticated', username: 'admin-test', csrf_token: 'mock-csrf', idle_timeout_seconds: 3600 };

function reading(s: Scenario): MetricsResponse {
  const m = structuredClone(piMetrics);
  m.collected_at = new Date().toISOString();
  m.routing = structuredClone(s.routing);
  return m;
}

/** A stand-in for EventSource on /api/v1/events: opens, then one metrics event every 2 s. */
function fakeEventSource(s: Scenario) {
  return class MockEventSource {
    static readonly CLOSED = 2;
    readyState = 0;
    onopen: ((ev: Event) => void) | null = null;
    onerror: ((ev: Event) => void) | null = null;
    private listeners: Record<string, ((ev: MessageEvent<string>) => void)[]> = {};
    private timer: ReturnType<typeof setInterval> | undefined;

    constructor(_url: string) {
      setTimeout(() => {
        if (this.readyState === 2) return;
        this.readyState = 1;
        this.onopen?.(new Event('open'));
        this.emit('history', piMetrics.history);
        this.emit('alerts', []);
        this.emit('metrics', reading(s));
        this.timer = setInterval(() => this.emit('metrics', reading(s)), 2000);
      }, 100);
    }

    addEventListener(type: string, listener: (ev: MessageEvent<string>) => void): void {
      (this.listeners[type] ??= []).push(listener);
    }

    close(): void {
      this.readyState = 2;
      clearInterval(this.timer);
    }

    private emit(type: string, data: unknown): void {
      const ev = new MessageEvent<string>(type, { data: JSON.stringify(data) });
      for (const l of this.listeners[type] ?? []) l(ev);
    }
  };
}

/** Replaces window.fetch and window.EventSource for these routes. Unknown scenario: nothing is replaced. */
export function installMetricsMock(name: string): boolean {
  const make = scenarios[name];
  if (!make) return false;
  const s = make();
  const real = window.fetch.bind(window);
  window.fetch = async (input: RequestInfo | URL, init?: RequestInit) => {
    const href = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
    const url = new URL(href, location.href);
    const method = (init?.method ?? (input instanceof Request ? input.method : 'GET')).toUpperCase();
    if (url.origin === location.origin && method === 'GET') {
      if (url.pathname === '/api/v1/session') return json(session);
      if (url.pathname === '/api/v1/metrics') return json(reading(s));
      if (url.pathname === '/api/v1/device') return json(s.device);
    }
    return real(input, init);
  };
  window.EventSource = fakeEventSource(s) as unknown as typeof EventSource;
  console.info(`[${devMockMarker}] metrics scenario "${name}"`);
  return true;
}
