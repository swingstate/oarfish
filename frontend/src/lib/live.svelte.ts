// The live snapshot of a page other than the Dashboard (tasks N1 and ST1): the
// same data path as the Dashboard, one snapshot from GET /api/v1/metrics for
// the first paint and then the metrics event stream, which the service samples
// only while somebody watches (ADR 0008 decision 4). The page calls start() in
// onMount and the returned function in its cleanup; a hidden tab closes the
// stream as on the Dashboard. Alerts and history are not used here.
import { computeRates, fetchMetrics, type Rate, type Snapshot } from './metrics';
import { fetchSession, sessionLost } from './session';
import { MetricsStream, type StreamState } from './stream';

export class LiveSnapshot {
  snapshot = $state<Snapshot | null>(null);
  rates = $state<Record<string, Rate>>({});
  conn = $state<StreamState>('connecting');
  loadFailed = $state(false);
  // The tab was hidden and no value has arrived since.
  afterPause = $state(false);

  private lastAt = 0;
  private loading = false;

  /** The values are old: the connection is troubled, or the tab came back and nothing new arrived yet. */
  get stale(): boolean {
    return this.snapshot !== null && (this.conn === 'stale' || this.conn === 'reconnecting' || this.afterPause);
  }

  /** The connection is troubled (the notice above the page). */
  get troubled(): boolean {
    return this.snapshot !== null && (this.conn === 'stale' || this.conn === 'reconnecting');
  }

  private apply(s: Snapshot): void {
    const at = Date.parse(s.collected_at);
    if (at <= this.lastAt) return; // an older reading must not replace a newer one
    this.lastAt = at;
    this.rates = computeRates(this.snapshot, s);
    this.snapshot = s;
  }

  async load(): Promise<void> {
    if (this.loading) return;
    this.loading = true;
    this.loadFailed = false;
    const r = await fetchMetrics();
    this.loading = false;
    if (!r.ok) {
      this.loadFailed = true;
      return;
    }
    this.apply(r.metrics);
    if (this.conn !== 'paused') this.afterPause = false;
  }

  start(): () => void {
    const stream = new MetricsStream(
      {
        onSnapshot: (s) => {
          this.afterPause = false;
          this.apply(s);
        },
        onAlerts: () => {},
        onHistory: () => {},
        onState: (s) => {
          this.conn = s;
          if (s === 'paused') this.afterPause = true;
          if (s === 'live' && this.snapshot === null) void this.load();
        },
        onSessionEnded: () => sessionLost('expired'),
      },
      {
        checkSession: async () => {
          const r = await fetchSession();
          if (!r.ok || r.session.state === 'authenticated') return true;
          sessionLost(r.session.state === 'setup_required' ? 'setup_required' : 'expired');
          return false;
        },
      },
    );
    stream.start();
    void this.load();
    return () => stream.stop();
  }
}
