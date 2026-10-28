import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { FakeEventSource } from './eventsource.fake';
import { piHistory, piSnapshot, genericModeAlert } from './metrics.fixtures';
import { MetricsStream, type StreamState } from './stream';

let hidden = false;
const doc = new EventTarget() as unknown as Pick<Document, 'addEventListener' | 'removeEventListener'>;
const setHidden = (value: boolean) => {
  hidden = value;
  (doc as unknown as EventTarget).dispatchEvent(new Event('visibilitychange'));
};

function setup(options: ConstructorParameters<typeof MetricsStream>[1] = {}) {
  const states: StreamState[] = [];
  const snapshots: unknown[] = [];
  const alerts: unknown[] = [];
  const histories: unknown[] = [];
  const malformed: string[] = [];
  const stream = new MetricsStream(
    {
      onSnapshot: (s) => snapshots.push(s),
      onAlerts: (a) => alerts.push(a),
      onHistory: (h) => histories.push(h),
      onState: (s) => states.push(s),
      onMalformed: (e) => malformed.push(e),
    },
    { createSource: (url) => new FakeEventSource(url), isHidden: () => hidden, doc, ...options },
  );
  return { stream, states, snapshots, alerts, histories, malformed };
}

beforeEach(() => {
  vi.useFakeTimers();
  FakeEventSource.reset();
  hidden = false;
});
afterEach(() => vi.useRealTimers());

describe('MetricsStream', () => {
  it('opens the event stream, reports connecting then live, and delivers parsed events', () => {
    const { stream, states, snapshots, alerts, histories } = setup();
    stream.start();
    expect(FakeEventSource.instances).toHaveLength(1);
    expect(FakeEventSource.last.url).toBe('/api/v1/events');
    expect(states).toEqual(['connecting']);

    FakeEventSource.last.open();
    FakeEventSource.last.send('history', piHistory);
    FakeEventSource.last.send('alerts', { schema_version: 1, alerts: [genericModeAlert] });
    FakeEventSource.last.send('metrics', piSnapshot);

    expect(states).toEqual(['connecting', 'live']);
    expect(snapshots).toEqual([piSnapshot]);
    expect(alerts).toEqual([[genericModeAlert]]);
    expect(histories).toEqual([piHistory]);
    stream.stop();
  });

  it('ignores a malformed event, reports it, and keeps working', () => {
    const { stream, snapshots, alerts, histories, malformed, states } = setup();
    stream.start();
    FakeEventSource.last.open();
    for (const bad of ['{"broken"', '', 'null', '[]', '"text"', '{"schema_version":2}', JSON.stringify({ ...piSnapshot, cpu: null })]) {
      expect(() => FakeEventSource.last.send('metrics', bad)).not.toThrow();
    }
    FakeEventSource.last.send('alerts', '{"nope":1}');
    FakeEventSource.last.send('history', 'not json');
    expect(snapshots).toEqual([]);
    expect(alerts).toEqual([]);
    expect(histories).toEqual([]);
    expect(malformed).toEqual(['metrics', 'metrics', 'metrics', 'metrics', 'metrics', 'metrics', 'metrics', 'alerts', 'history']);
    expect(states.at(-1)).toBe('live');

    FakeEventSource.last.send('metrics', piSnapshot);
    expect(snapshots).toEqual([piSnapshot]);
    stream.stop();
  });

  it('marks the values stale when an open stream delivers nothing, and recovers on the next value', () => {
    const { stream, states } = setup({ staleAfterMs: 7000 });
    stream.start();
    FakeEventSource.last.open();
    FakeEventSource.last.send('metrics', piSnapshot);

    vi.advanceTimersByTime(6999);
    expect(states.at(-1)).toBe('live');
    vi.advanceTimersByTime(1);
    expect(states.at(-1)).toBe('stale');

    FakeEventSource.last.send('metrics', { ...piSnapshot, collected_at: '2026-10-27T12:00:09.000Z' });
    expect(states.at(-1)).toBe('live');
    stream.stop();
  });

  it('lets the browser retry a dropped connection and shows it as reconnecting', () => {
    const { stream, states } = setup();
    stream.start();
    FakeEventSource.last.open();
    FakeEventSource.last.dropAndRetry();
    expect(states.at(-1)).toBe('reconnecting');
    expect(FakeEventSource.instances).toHaveLength(1);
    vi.advanceTimersByTime(60000);
    expect(FakeEventSource.instances).toHaveLength(1); // no second connection of ours
    FakeEventSource.last.open(); // the browser got through
    expect(states.at(-1)).toBe('live');
    stream.stop();
  });

  it('reconnects by hand when the connection is refused, with a growing delay that resets after a success', () => {
    const { stream, states } = setup({ baseDelayMs: 3000, maxDelayMs: 10000 });
    stream.start();
    FakeEventSource.last.fail();
    expect(FakeEventSource.last.closed).toBe(true);
    expect(states.at(-1)).toBe('reconnecting');

    vi.advanceTimersByTime(2999);
    expect(FakeEventSource.instances).toHaveLength(1);
    vi.advanceTimersByTime(1);
    expect(FakeEventSource.instances).toHaveLength(2);

    FakeEventSource.last.fail(); // second failure: 6 s
    vi.advanceTimersByTime(5999);
    expect(FakeEventSource.instances).toHaveLength(2);
    vi.advanceTimersByTime(1);
    expect(FakeEventSource.instances).toHaveLength(3);

    FakeEventSource.last.fail(); // third: 12 s, capped at 10 s
    vi.advanceTimersByTime(9999);
    expect(FakeEventSource.instances).toHaveLength(3);
    vi.advanceTimersByTime(1);
    expect(FakeEventSource.instances).toHaveLength(4);

    FakeEventSource.last.open(); // success resets the delay
    FakeEventSource.last.send('metrics', piSnapshot);
    expect(states.at(-1)).toBe('live');
    FakeEventSource.last.fail();
    vi.advanceTimersByTime(3000);
    expect(FakeEventSource.instances).toHaveLength(5);
    stream.stop();
  });

  it('closes the stream when the tab is hidden and opens it again when it is shown (ORB-NFR-009)', () => {
    const { stream, states } = setup();
    stream.start();
    const first = FakeEventSource.last;
    first.open();

    setHidden(true);
    expect(first.closed).toBe(true);
    expect(states.at(-1)).toBe('paused');
    expect(vi.getTimerCount()).toBe(0); // no stale timer, no retry timer while hidden
    first.send('metrics', piSnapshot); // a late event from the closed source is ignored
    vi.advanceTimersByTime(60000);
    expect(FakeEventSource.instances).toHaveLength(1);

    setHidden(false);
    expect(FakeEventSource.instances).toHaveLength(2);
    expect(states.at(-1)).toBe('connecting');
    stream.stop();
  });

  it('does not open a stream at all while the tab is hidden', () => {
    hidden = true;
    const { stream, states } = setup();
    stream.start();
    expect(FakeEventSource.instances).toHaveLength(0);
    expect(states).toEqual(['paused']);
    setHidden(false);
    expect(FakeEventSource.instances).toHaveLength(1);
    stream.stop();
  });

  it('drops a pending retry when the tab is hidden', () => {
    const { stream } = setup();
    stream.start();
    FakeEventSource.last.fail();
    setHidden(true);
    vi.advanceTimersByTime(60000);
    expect(FakeEventSource.instances).toHaveLength(1);
    stream.stop();
  });

  it('stop() closes the stream, removes its timers and listener, and is repeatable', () => {
    const { stream, snapshots } = setup();
    stream.start();
    FakeEventSource.last.open();
    FakeEventSource.last.send('metrics', piSnapshot);
    stream.stop();
    expect(FakeEventSource.last.closed).toBe(true);
    expect(vi.getTimerCount()).toBe(0);
    setHidden(true);
    setHidden(false);
    expect(FakeEventSource.instances).toHaveLength(1); // the visibility listener is gone
    stream.stop();
    expect(snapshots).toHaveLength(1);
  });

  it('start() twice opens one stream', () => {
    const { stream } = setup();
    stream.start();
    stream.start();
    expect(FakeEventSource.instances).toHaveLength(1);
    stream.stop();
  });
});
