// A stand-in for EventSource in tests: the test decides what the server sends.
import type { EventSourceLike } from './stream';

export class FakeEventSource implements EventSourceLike {
  static instances: FakeEventSource[] = [];
  static reset(): void {
    FakeEventSource.instances = [];
  }
  static get last(): FakeEventSource {
    return FakeEventSource.instances[FakeEventSource.instances.length - 1]!;
  }

  readyState = 0; // CONNECTING
  closed = false;
  onopen: ((ev: Event) => void) | null = null;
  onerror: ((ev: Event) => void) | null = null;
  private listeners = new Map<string, ((ev: MessageEvent<string>) => void)[]>();

  constructor(readonly url: string) {
    FakeEventSource.instances.push(this);
  }

  addEventListener(type: string, listener: (ev: MessageEvent<string>) => void): void {
    this.listeners.set(type, [...(this.listeners.get(type) ?? []), listener]);
  }

  close(): void {
    this.closed = true;
    this.readyState = 2;
  }

  // ---- what the test makes the server do ----
  open(): void {
    this.readyState = 1;
    this.onopen?.(new Event('open'));
  }

  send(type: string, data: unknown): void {
    const text = typeof data === 'string' ? data : JSON.stringify(data);
    for (const l of this.listeners.get(type) ?? []) l(new MessageEvent(type, { data: text }));
  }

  /** The connection dropped; the browser retries by itself. */
  dropAndRetry(): void {
    this.readyState = 0;
    this.onerror?.(new Event('error'));
  }

  /** The server refused (for example 503); the browser does not retry. */
  fail(): void {
    this.readyState = 2;
    this.onerror?.(new Event('error'));
  }
}
