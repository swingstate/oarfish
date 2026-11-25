// Synthetic answers of GET /api/v1/power for the tests and the development
// scenario mode of the Activity page (G7.5). No host name, user name, address
// or other device value is here, and no password: the tests type their own
// visibly fake one into the field.
import type { PowerState, WaitingItem } from './power';

export function powerState(waiting: WaitingItem[] = [], brokerAvailable = true): PowerState {
  return { schema_version: 1, broker_available: brokerAvailable, waiting };
}

/** One item of every kind the service sends, the counted ones above one, and a kind this version does not know. */
export function everyWaitingKind(): WaitingItem[] {
  return [
    { kind: 'update_staged' },
    { kind: 'update_running' },
    { kind: 'service_action_running' },
    { kind: 'other_sessions', count: 2 },
    { kind: 'other_views', count: 3 },
    { kind: 'future_kind' },
  ];
}

/** The counted kinds with a count of one (the singular sentences). */
export function singleCounts(): WaitingItem[] {
  return [
    { kind: 'other_sessions', count: 1 },
    { kind: 'other_views', count: 1 },
  ];
}
