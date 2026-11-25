// Sentences for the result codes of a service action (ADR 0011 B2 as amended by
// A3.3; backend/internal/brokerclient). Every code has its own sentence in both
// languages; anything else gets the generic sentence. The server's English
// message is never shown. A state word is translated from its closed set,
// never shown raw.
import { durationText } from './updateErrors';
import { fmt, has, msg, t } from './i18n';
import { activeStates, criticalKinds, startModes } from './services';

/** The codes of a finished action and of the action route, each with a sentence. */
export const serviceCodes = [
  'bad_request',
  'busy',
  'rate_limited',
  'unit_invalid',
  'unit_unknown',
  'unit_not_canonical',
  'unit_denied',
  'unit_masked',
  'unit_transient',
  'unit_oneshot',
  'unit_template',
  'refused_by_unit',
  'operation_not_offered',
  'already_in_state',
  'job_failed',
  'timeout',
  'state_unexpected',
  'internal',
  'unavailable',
  'invalid_request',
] as const;

const known = <T extends string>(list: readonly T[], v: string): v is T => (list as readonly string[]).includes(v);

/** A state word in words; a word this version does not know reads "Unbekannt". */
export function stateText(state: string | undefined): string {
  return state && known(activeStates, state) ? msg(`actState.${state}`) : t.unknownValue;
}

/** A start mode in words; an empty or unknown word reads "Unbekannt". */
export function startModeText(mode: string): string {
  return known(startModes, mode) ? msg(`actMode.${mode}`) : t.unknownValue;
}

/** The note of a critical kind, or null for a kind this version does not know. */
export function criticalNote(kind: string): string | null {
  return known(criticalKinds, kind) ? msg(`actCritical.${kind}`) : null;
}

/** The short name of a critical kind for the row; an unknown kind gets the generic word. */
export function kindText(kind: string): string {
  return known(criticalKinds, kind) ? msg(`actKind.${kind}`) : t['actKind.other'];
}

/** The sentence for the result of a finished action. Success says that a later failure is not verified (A3.3). */
export function resultText(code: string | undefined, state: string | undefined, retryAfter: number | undefined): string {
  if (code === 'success') return fmt('actResultSuccess', { state: stateText(state) });
  return errorText(code ?? '', state, retryAfter ?? null);
}

/** The sentence for an error code: of a finished action, or of the action route itself. */
export function errorText(code: string, state?: string, retryAfter: number | null = null): string {
  if (code === 'rate_limited') return fmt('svcErr.rate_limited', { duration: durationText(retryAfter ?? 60) });
  if (code === 'job_failed' || code === 'state_unexpected') return fmt(`svcErr.${code}`, { state: stateText(state) });
  const key = `svcErr.${code}`;
  return known(serviceCodes, code) && has(key) ? msg(key) : t.errorGeneric;
}
