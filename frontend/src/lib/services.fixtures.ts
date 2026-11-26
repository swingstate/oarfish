// Synthetic service lists and last actions for the tests and the development
// scenario mode of the Activity page (G7.4). Ordinary rows use invented unit
// names; the critical and restricted rows use the list names of ADR 0011 B3
// rules 10 and 11, which are systemd's and the distributions' names, not device
// values. No host name, user name, address or other device value is here.
import type { ActionState, ActionView, ServiceRow, ServicesList } from './services';

export const fixtureReadAt = '2026-11-25T09:30:00Z';

interface RowOptions {
  description?: string;
  load?: string;
  sub?: string;
  denied?: boolean;
  restartOnly?: boolean;
  critical?: string;
}

/** A row with the actions the service would offer (backend/internal/services/classify.go). */
export function row(name: string, active: string, mode: string, o: RowOptions = {}): ServiceRow {
  let actions: string[] = [];
  if (o.denied) actions = [];
  else if (o.restartOnly) actions = ['restart'];
  else {
    if (active !== 'active') actions.push('start');
    if (active === 'active' || active === 'activating' || active === 'reloading') actions.push('stop');
    actions.push('restart');
  }
  return {
    name,
    description: o.description ?? '',
    load_state: o.load ?? 'loaded',
    active_state: active,
    sub_state: o.sub ?? (active === 'active' ? 'running' : active === 'failed' ? 'failed' : 'dead'),
    start_mode: mode,
    denied: !!o.denied,
    restart_only: !!o.restartOnly,
    critical: o.critical ?? '',
    actions,
  };
}

function list(rows: ServiceRow[], brokerAvailable = true): ServicesList {
  const sorted = [...rows].sort((a, b) => (a.name < b.name ? -1 : a.name > b.name ? 1 : 0));
  return {
    schema_version: 1,
    read_at: fixtureReadAt,
    broker_available: brokerAvailable,
    counts: {
      total: sorted.length,
      active: sorted.filter((r) => r.active_state === 'active').length,
      failed: sorted.filter((r) => r.active_state === 'failed').length,
      inactive: sorted.filter((r) => r.active_state === 'inactive').length,
    },
    services: sorted,
  };
}

/** The ordinary rows: invented names, every start mode, one instance. */
export function ordinaryRows(): ServiceRow[] {
  return [
    row('demo-app.service', 'active', 'enabled', { description: 'Demo application server' }),
    row('demo-batch.service', 'inactive', 'static', { description: 'Demo nightly batch' }),
    row('demo-cache.service', 'active', 'enabled-runtime', { description: 'Demo cache' }),
    row('demo-idle.service', 'inactive', 'disabled', { load: 'not-loaded' }),
    row('demo-masked.service', 'inactive', 'masked', { load: 'masked', description: 'Demo service that is masked' }),
    row('demo-worker@one.service', 'active', 'indirect', { description: 'Demo worker one' }),
    row('example-linked.service', 'inactive', 'linked', { description: 'Example linked unit' }),
    row('example-generated.service', 'active', 'generated', { description: 'Example unit from a generator', sub: 'exited' }),
    row('example-run.service', 'activating', 'transient', { description: 'Example unit made at run time', sub: 'start' }),
    row('example-web.service', 'reloading', '', { description: 'Example web server with no unit file state', sub: 'reload' }),
  ];
}

/** The rows of rule 11 (critical, some restart only) and rule 10 (denied). */
export function specialRows(): ServiceRow[] {
  return [
    row('ssh.service', 'active', 'enabled', { description: 'OpenBSD Secure Shell server', restartOnly: true, critical: 'ssh' }),
    row('NetworkManager.service', 'active', 'enabled', { description: 'Network Manager', restartOnly: true, critical: 'network' }),
    row('avahi-daemon.service', 'active', 'enabled', { description: 'Avahi mDNS/DNS-SD Stack', restartOnly: true, critical: 'network' }),
    row('tailscaled.service', 'active', 'enabled', { description: 'Tailscale node agent', restartOnly: true, critical: 'remote_access' }),
    row('docker.service', 'active', 'enabled', { description: 'Docker Application Container Engine', critical: 'container' }),
    row('chrony.service', 'active', 'enabled', { description: 'chrony, an NTP client/server', critical: 'time' }),
    row('dbus.service', 'active', 'static', { description: 'D-Bus System Message Bus', denied: true }),
    row('systemd-journald.service', 'active', 'static', { description: 'Journal Service', denied: true }),
    row('getty@tty1.service', 'active', 'enabled', { description: 'Getty on tty1', denied: true }),
    row('oarfish.service', 'active', 'enabled', { description: 'Oarfish', denied: true }),
  ];
}

/** The normal list: ordinary and special rows, broker reachable. */
export function sampleList(): ServicesList {
  return list([...ordinaryRows(), ...specialRows()]);
}

/** A list with three failed services. */
export function failedList(): ServicesList {
  return list([
    ...ordinaryRows(),
    ...specialRows(),
    row('demo-broken.service', 'failed', 'enabled', { description: 'Demo service that failed to start' }),
    row('example-crash.service', 'failed', 'disabled', { description: 'Example service that crashed' }),
    row('sample-sync.service', 'failed', 'static', { description: 'Sample sync job' }),
  ]);
}

/** Exactly one row in transition (activating), as on the device in G7.6c: the count line names the group. */
export function transitionList(): ServicesList {
  return list(ordinaryRows().filter((r) => r.active_state !== 'reloading'));
}

/** The list without the broker's socket: the rows stay, no action works. */
export function brokerMissingList(): ServicesList {
  return list([...ordinaryRows(), ...specialRows()], false);
}

/** A list with no failed row, to see the empty filter state with "Fehlgeschlagen". */
export function noFailedList(): ServicesList {
  return list(ordinaryRows().filter((r) => r.active_state !== 'failed'));
}

/** A long list: more than one page of 100 rows. */
export function longList(n = 250): ServicesList {
  const rows: ServiceRow[] = [];
  for (let i = 0; i < n; i++) {
    const id = String(i).padStart(3, '0');
    rows.push(row(`demo-unit-${id}.service`, i % 7 === 0 ? 'failed' : i % 3 === 0 ? 'inactive' : 'active', i % 2 ? 'enabled' : 'disabled', { description: `Demo unit number ${id}` }));
  }
  return list(rows);
}

/** A row whose name and description carry markup: it must show as text. */
export function hostileList(): ServicesList {
  return list([
    row('demo-markup.service', 'active', 'enabled', { description: '<img src=x onerror="window.__pwned=1">Demo <b>bold</b>' }),
    row('demo-state.service', 'zombie', 'enabled-forever', { description: 'A state and a mode this version does not know', critical: 'quantum' }),
  ]);
}

export function running(unit: string, action: string): ActionState {
  return { schema_version: 1, action: { unit, action, step: 'running', started_at: '2026-11-25T09:31:00Z' } };
}

export function done(unit: string, action: string, code: string, extra: Partial<ActionView> = {}): ActionState {
  return {
    schema_version: 1,
    action: { unit, action, step: 'done', started_at: '2026-11-25T09:31:00Z', finished_at: '2026-11-25T09:31:03Z', code, ...extra },
  };
}

export const noAction: ActionState = { schema_version: 1, action: null };
