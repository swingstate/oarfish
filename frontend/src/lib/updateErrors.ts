// Sentences for the error codes of the update routes (ADR 0010 U2, U8;
// backend/internal/updater/errors.go). The set is closed: every code has its
// own sentence in both languages, http_<three digits> shares one, and anything
// else gets the generic sentence. The server's English message is never shown.
import { waitText } from './authErrors';
import { fmt, has, msg, t } from './i18n';

export const updateCodes = [
  'bad_request',
  'busy',
  'not_found',
  'signature_invalid',
  'checksum_mismatch',
  'manifest_invalid',
  'downgrade_refused',
  'already_installed',
  'requirements_not_met',
  'helper_too_old',
  'installer_required',
  'confirmation_required',
  'key_changed',
  'unit_rejected',
  'components_damaged',
  'stage_expired',
  'no_space',
  'current_unhealthy',
  'cooldown',
  'io_error',
  'internal',
  'unavailable',
  'not_checked',
  'version_mismatch',
  'version_unknown',
  'too_large',
  'cache_error',
  'bad_response',
  'dns_failure',
  'tls_failure',
  'timeout',
  'unreachable',
  'rate_limited',
  'redirect_refused',
  'cancelled',
] as const;

export type UpdateCode = (typeof updateCodes)[number];

const isUpdateCode = (code: string): code is UpdateCode => (updateCodes as readonly string[]).includes(code);

/** The sentence for an error code inside the State (check, verification, job, helper). */
export function updateErrorText(code: string): string {
  if (isUpdateCode(code)) return t[`updErr.${code}`];
  if (/^http_\d{3}$/.test(code)) return t['updErr.http'];
  return t.errorGeneric;
}

/** A wait in the unit a person reads best, in the dative after "in": "einer Sekunde", "42 Sekunden", "3 Minuten". */
export function durationText(seconds: number): string {
  const s = Math.max(1, Math.ceil(seconds));
  if (s === 1) return t.durOneSecond;
  if (s < 120) return fmt('durSeconds', { n: s });
  const m = Math.ceil(s / 60);
  return fmt('durMinutes', { n: m });
}

/** Which field an error of a confirmation belongs to: only the password field can be wrong. */
export type UpdateErrorField = 'password' | null;

/**
 * The sentence for the HTTP answer of check, apply or discard. These routes
 * can also answer with the codes of the access layer (wrong password, rate
 * limit, origin and CSRF checks) and of the body reader.
 */
export function actionErrorText(code: string, retryAfter: number | null = null): { text: string; field: UpdateErrorField } {
  switch (code) {
    case 'wrong_password':
      return { text: t.errorCurrentPassword, field: 'password' };
    case 'rate_limited':
      // The password limiter and GitHub's limit share the code; the sentence fits both.
      return { text: waitText(retryAfter), field: null };
    case 'network':
      return { text: t.errorNetwork, field: null };
    case 'origin_rejected':
    case 'csrf_failed':
      return { text: t['updErr.request_rejected'], field: null };
    case 'request_too_large':
      return { text: t['updErr.request_too_large'], field: null };
    case 'invalid_request':
      return { text: t['updErr.bad_request'], field: null };
    case 'update_unavailable':
      return { text: t['updErr.update_unavailable'], field: null };
    default:
      return { text: updateErrorText(code), field: null };
  }
}

/**
 * The reason sentence of the outcome components_rolled_back (ADR 0010 A16, the
 * closed set of eight reasons), in four groups: the new helper failed its check
 * at its first start; the transaction was interrupted; the state could not be
 * read; a step of the helper that handed over failed. An unknown reason has no
 * sentence: the code is still shown under the technical details.
 */
export function componentsReasonText(reason: string | undefined): string | null {
  switch (reason) {
    case 'helper_mismatch':
    case 'key_changed':
    case 'protocol_mismatch':
      return t['updComponentsReason.check'];
    case 'interrupted':
      return t['updComponentsReason.interrupted'];
    case 'state_unreadable':
      return t['updComponentsReason.state'];
    case 'switch_failed':
    case 'state_write_failed':
    case 'reload_failed':
      return t['updComponentsReason.switch'];
    default:
      return null;
  }
}

/** The plain words for a component name of the closed list; null for a name this version does not know. */
export function componentText(name: string): string | null {
  const key = `updComponent.${name}`;
  return has(key) ? msg(key) : null;
}

/** The reason sentence of the installer view; a service that sends no code is a helper before G6.10 step b. */
export function installerReasonText(code: string | undefined): string {
  const key = `updInstallerReason.${code ?? 'installer_required'}`;
  return has(key) ? msg(key) : t['updInstallerReason.installer_required'];
}
