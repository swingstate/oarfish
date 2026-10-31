// The live connection to GET /api/v1/events (Server-Sent Events).
//
// The server only samples the device while somebody listens, so this class
// listens only while the page is visible: it closes the stream when the tab is
// hidden and opens it again when the tab is shown. It reconnects after errors
// (the browser does that itself for a dropped connection; for a refused one,
// for example 503 when too many windows are open, it must be done by hand),
// and it reports a "stale" state when the stream is open but no values arrive.
//
// The stream belongs to a session (ADR 0009, E2). When the session ends, the
// server sends a last "session" event and closes; this class then stops for
// good and reports it, instead of reconnecting. When the browser gives up on a
// refused connection, the class asks checkSession before it tries again: a 401
// (session gone) must not turn into a reconnect loop.
import { parseAlerts, parseHistory, parseSnapshot, type Alert, type History, type Snapshot } from './metrics';

export type StreamState =
  | 'connecting' // opening for the first time
  | 'live' // values are arriving
  | 'stale' // open, but no values for a while
  | 'reconnecting' // the connection broke; trying again
  | 'paused'; // the tab is hidden; nothing is open

export interface StreamHandlers {
  onSnapshot(snapshot: Snapshot): void;
  onAlerts(alerts: Alert[]): void;
  onHistory(history: History): void;
  onState(state: StreamState): void;
  /** A message that could not be read and was ignored. */
  onMalformed?(event: string): void;
  /** The session of the stream ended; the stream has stopped and does not reconnect. */
  onSessionEnded?(): void;
}

/** The part of EventSource this class uses, so tests can supply a fake. */
export interface EventSourceLike {
  readonly readyState: number;
  onopen: ((ev: Event) => void) | null;
  onerror: ((ev: Event) => void) | null;
  addEventListener(type: string, listener: (ev: MessageEvent<string>) => void): void;
  close(): void;
}

export interface StreamOptions {
  url?: string;
  /** No value for this long in an open stream marks the values stale. The server sends every 2 s. */
  staleAfterMs?: number;
  /** First and largest delay when reconnecting by hand; the delay doubles. */
  baseDelayMs?: number;
  maxDelayMs?: number;
  createSource?: (url: string) => EventSourceLike;
  isHidden?: () => boolean;
  doc?: Pick<Document, 'addEventListener' | 'removeEventListener'>;
  /** Asked before a reconnect by hand; false means the session is gone and the stream stops. */
  checkSession?: () => Promise<boolean>;
}

const CLOSED = 2; // EventSource.CLOSED

export class MetricsStream {
  private readonly o: Required<Omit<StreamOptions, 'checkSession'>>;
  private readonly checkSession: (() => Promise<boolean>) | undefined;
  private source: EventSourceLike | null = null;
  private staleTimer: ReturnType<typeof setTimeout> | null = null;
  private retryTimer: ReturnType<typeof setTimeout> | null = null;
  private attempt = 0;
  private running = false;
  private current: StreamState | null = null;

  constructor(private readonly h: StreamHandlers, options: StreamOptions = {}) {
    this.o = {
      url: '/api/v1/events',
      staleAfterMs: 7000,
      baseDelayMs: 3000,
      maxDelayMs: 30000,
      createSource: (url) => new EventSource(url) as unknown as EventSourceLike,
      isHidden: () => document.hidden,
      doc: document,
      ...options,
    };
    this.checkSession = options.checkSession;
  }

  /** Starts listening, now if the page is visible, otherwise when it becomes visible. */
  start(): void {
    if (this.running) return;
    this.running = true;
    this.o.doc.addEventListener('visibilitychange', this.onVisibility);
    this.onVisibility();
  }

  /** Closes the stream and removes every listener and timer. */
  stop(): void {
    this.running = false;
    this.o.doc.removeEventListener('visibilitychange', this.onVisibility);
    this.close();
  }

  private readonly onVisibility = (): void => {
    if (!this.running) return;
    if (this.o.isHidden()) {
      this.close();
      this.setState('paused');
    } else if (!this.source) {
      this.attempt = 0;
      this.open();
    }
  };

  private setState(s: StreamState): void {
    if (this.current === s) return;
    this.current = s;
    this.h.onState(s);
  }

  private open(): void {
    this.clearRetry();
    this.setState(this.attempt === 0 ? 'connecting' : 'reconnecting');
    const es = this.o.createSource(this.o.url);
    this.source = es;
    es.onopen = () => {
      if (this.source !== es) return;
      this.attempt = 0;
      this.setState('live');
      this.armStale();
    };
    es.onerror = () => {
      if (this.source !== es) return;
      this.clearStale();
      this.setState('reconnecting');
      if (es.readyState === CLOSED) {
        // The browser gave up (for example on a 503, or a 401 when the session
        // is gone). Ask whether the session still exists; if so, try again by
        // hand, later each time.
        es.close();
        this.source = null;
        const delay = Math.min(this.o.baseDelayMs * 2 ** this.attempt, this.o.maxDelayMs);
        this.attempt++;
        const retry = () => {
          this.retryTimer = setTimeout(() => {
            this.retryTimer = null;
            if (this.running && !this.o.isHidden()) this.open();
          }, delay);
        };
        if (!this.checkSession) {
          retry();
          return;
        }
        void this.checkSession().then((alive) => {
          if (!this.running || this.source !== null) return;
          if (alive) retry();
          else this.endSession();
        });
      }
      // Otherwise the browser is reconnecting itself after the server's retry hint.
    };
    const on = (type: string, handle: (data: string) => boolean) =>
      es.addEventListener(type, (ev) => {
        if (this.source !== es) return;
        if (!handle(String(ev.data))) this.h.onMalformed?.(type);
      });
    on('metrics', (data) => {
      const s = parseSnapshot(data);
      if (!s) return false;
      this.h.onSnapshot(s);
      this.setState('live');
      this.armStale();
      return true;
    });
    on('alerts', (data) => {
      const a = parseAlerts(data);
      if (!a) return false;
      this.h.onAlerts(a);
      return true;
    });
    on('history', (data) => {
      const hist = parseHistory(data);
      if (!hist) return false;
      this.h.onHistory(hist);
      return true;
    });
    on('session', () => {
      this.endSession();
      return true;
    });
  }

  /** The session is gone: stop for good and say so. */
  private endSession(): void {
    this.stop();
    this.h.onSessionEnded?.();
  }

  private armStale(): void {
    this.clearStale();
    this.staleTimer = setTimeout(() => {
      this.staleTimer = null;
      if (this.source) this.setState('stale');
    }, this.o.staleAfterMs);
  }

  private clearStale(): void {
    if (this.staleTimer !== null) clearTimeout(this.staleTimer);
    this.staleTimer = null;
  }

  private clearRetry(): void {
    if (this.retryTimer !== null) clearTimeout(this.retryTimer);
    this.retryTimer = null;
  }

  private close(): void {
    this.clearStale();
    this.clearRetry();
    if (this.source) {
      this.source.onopen = null;
      this.source.onerror = null;
      this.source.close();
      this.source = null;
    }
  }
}
