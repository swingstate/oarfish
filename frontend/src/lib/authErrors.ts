// Sentences for the error codes of the access-control endpoints (docs/AUTH.md).
// A wrong user name and a wrong password get the same sentence (ADR 0009, E6).
import { fmt, t } from './i18n';

/** The rate-limit wait in the unit a person reads best. */
export function waitText(seconds: number | null): string {
  const s = Math.max(1, Math.ceil(seconds ?? 1));
  if (s === 1) return t.errorRateLimitedOneSecond;
  return s < 120 ? fmt('errorRateLimitedSeconds', { seconds: s }) : fmt('errorRateLimitedMinutes', { minutes: Math.ceil(s / 60) });
}

/** Which field an error belongs to, so the form can mark and focus it. */
export type ErrorField = 'token' | 'username' | 'password' | 'current' | null;

export function errorText(code: string, retryAfter: number | null = null): { text: string; field: ErrorField } {
  switch (code) {
    case 'invalid_credentials':
      return { text: t.errorCredentials, field: 'password' };
    case 'rate_limited':
      return { text: waitText(retryAfter), field: null };
    case 'invalid_setup_token':
      return { text: t.errorSetupToken, field: 'token' };
    case 'setup_not_available':
      return { text: t.errorSetupDone, field: null };
    case 'username_invalid':
      return { text: t.errorUsername, field: 'username' };
    case 'password_too_short':
      return { text: t.errorPasswordShort, field: 'password' };
    case 'password_too_long':
      return { text: t.errorPasswordLong, field: 'password' };
    case 'password_too_common':
      return { text: t.errorPasswordCommon, field: 'password' };
    case 'password_invalid':
      return { text: t.errorPasswordInvalid, field: 'password' };
    case 'password_mismatch':
      return { text: t.errorPasswordMismatch, field: 'password' };
    case 'wrong_password':
      return { text: t.errorCurrentPassword, field: 'current' };
    case 'network':
      return { text: t.errorNetwork, field: null };
    default:
      return { text: t.errorGeneric, field: null };
  }
}
