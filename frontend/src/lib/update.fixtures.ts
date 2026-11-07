// Synthetic update states, one per designed state of the update page
// (docs/UI.md, "Update page"). The tests render each of them, and the
// development scenario mode (src/dev/updateMock.ts) answers with them. Versions
// and texts are invented; nothing here names a device, a host or a person.
import type { UpdateState } from './update';

export const installedVersion = '0.1.0-alpha.7';
export const nextVersion = '0.1.0-alpha.8';

/** Release notes as GitHub might send them: plain text that also contains markup, which must stay literal. */
export const sampleNotes = [
  'Changes in this release',
  '',
  '- The update page shows installed and available version.',
  '- <b>Bold</b> and <script>alert(1)</script> stay plain text.',
  '- **Markdown** is not rendered either.',
  '',
  'Known limits: nothing of this is verified on a device yet.',
].join('\n');

const minute = 60_000;
const iso = (ms: number) => new Date(ms).toISOString().replace(/\.\d{3}Z$/, 'Z');

/** Checked five minutes ago, nothing newer; the next page check is due in 55 minutes. */
export function upToDate(now = Date.now()): UpdateState {
  return {
    schema_version: 1,
    installed_version: installedVersion,
    helper: { present: true, phase: 'idle' },
    check: { state: 'ok', checked_at: iso(now - 5 * minute), next_check_at: iso(now + 55 * minute) },
    available: null,
    job: null,
    last_result: null,
    installer: null,
  };
}

/** A copy of a state with a change applied. */
export function withState(base: UpdateState, change: (s: UpdateState) => void): UpdateState {
  const s = structuredClone(base);
  change(s);
  return s;
}

export function available(now = Date.now()): UpdateState {
  return withState(upToDate(now), (s) => {
    s.available = { version: nextVersion, channel: 'prerelease', notes: sampleNotes, download_size: 3_456_789, verified: true };
  });
}

const job = (now: number, step: NonNullable<UpdateState['job']>['step'], running = true) => ({
  kind: 'apply' as const,
  step,
  version: nextVersion,
  running,
  started_at: iso(now - minute),
  ...(running ? {} : { finished_at: iso(now) }),
});

/** Every designed state, by the name the scenario mode and the tests use. */
export function updateScenarios(now = Date.now()): Record<string, UpdateState> {
  const av = available(now);
  return {
    'no-helper': withState(av, (s) => {
      s.helper = { present: false, error: { code: 'unavailable', message: 'The update helper could not be reached.' } };
    }),
    never: withState(upToDate(now), (s) => {
      s.check = { state: 'never' };
    }),
    'up-to-date': upToDate(now),
    available: av,
    'available-stable': withState(av, (s) => {
      s.available!.channel = 'stable';
      s.available!.notes = '';
    }),
    'available-unverified': withState(av, (s) => {
      s.available!.verified = false;
      s.available!.verify_error = { code: 'signature_invalid', message: 'The release signature is not valid.' };
    }),
    'check-running': withState(upToDate(now), (s) => {
      s.job = { kind: 'check', step: 'checking', running: true, started_at: iso(now - 4000) };
    }),
    'check-failed': withState(upToDate(now), (s) => {
      s.check = {
        state: 'error',
        checked_at: iso(now - 70 * minute),
        error: { code: 'tls_failure', message: 'The secure connection to the update server failed.' },
        next_check_at: iso(now + 50 * minute),
      };
    }),
    'check-rate-limited': withState(upToDate(now), (s) => {
      s.check = {
        state: 'error',
        error: { code: 'rate_limited', message: 'The update server refuses more requests for now.', retry_after_seconds: 1500 },
        next_check_at: iso(now + 25 * minute),
      };
    }),
    throttled: withState(av, (s) => {
      s.check.throttled = true;
    }),
    'apply-downloading': withState(av, (s) => {
      s.job = job(now, 'downloading');
    }),
    'apply-activating': withState(av, (s) => {
      s.job = job(now, 'activating');
      s.helper.phase = 'switching';
    }),
    'awaiting-confirmation': withState(av, (s) => {
      s.job = job(now, 'awaiting_components_confirmation');
      s.helper.phase = 'staged';
      s.helper.staged_version = nextVersion;
    }),
    'installer-required': withState(av, (s) => {
      s.job = { ...job(now, 'failed', false), error: { code: 'installer_required', message: 'This release must be installed with the installer.' } };
      s.installer = {
        required: true,
        version: nextVersion,
        instruction: 'This release replaces privileged parts of Oarfish, so it cannot be applied from this page.',
        differs: ['oarfish-update-helper', 'oarfish.service'],
      };
    }),
    'apply-failed': withState(av, (s) => {
      s.job = { ...job(now, 'failed', false), error: { code: 'timeout', message: 'The update server did not answer in time.' } };
    }),
    'result-success': withState(upToDate(now), (s) => {
      s.installed_version = nextVersion;
      s.last_result = { to: nextVersion, from: installedVersion, outcome: 'success', finished_at: iso(now - 2 * minute) };
      s.helper.phase = 'committed';
    }),
    'result-rolled-back': withState(av, (s) => {
      s.last_result = { to: nextVersion, from: installedVersion, outcome: 'rolled_back', reason_code: 'unhealthy', finished_at: iso(now - 2 * minute) };
      s.helper = { present: true, phase: 'rolled_back', cooldown_version: nextVersion, cooldown_remaining_s: 1700 };
    }),
    'result-components-rolled-back': withState(av, (s) => {
      s.last_result = { to: nextVersion, from: installedVersion, outcome: 'components_rolled_back', reason_code: 'key_changed', finished_at: iso(now - 2 * minute) };
    }),
    'result-rollback-failed': withState(av, (s) => {
      s.last_result = { to: nextVersion, from: installedVersion, outcome: 'rollback_failed', reason_code: 'unhealthy', finished_at: iso(now - 2 * minute) };
      s.helper = { present: true, phase: 'rollback_failed', needs_attention: true };
    }),
    'result-aborted': withState(av, (s) => {
      s.last_result = { to: nextVersion, from: installedVersion, outcome: 'aborted', reason_code: 'interrupted', finished_at: iso(now - 2 * minute) };
    }),
    cooldown: withState(av, (s) => {
      s.helper = { present: true, phase: 'idle', cooldown_version: nextVersion, cooldown_remaining_s: 1700 };
    }),
    'needs-attention': withState(av, (s) => {
      s.helper = { present: true, phase: 'idle', needs_attention: true };
    }),
    staged: withState(av, (s) => {
      s.helper = { present: true, phase: 'staged', staged_version: nextVersion };
    }),
  };
}
