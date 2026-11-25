// Sentences for the power routes (ADR 0011 B2 and B6; backend/internal/server
// power.go). Every code has its own sentence in both languages; the password
// codes and the codes of the access layer reuse the sentences of the update
// page's password step; anything else gets the generic sentence. The server's
// English message is never shown.
import { actionErrorText, type UpdateErrorField } from './updateErrors';
import { fmt, has, msg, t } from './i18n';
import { waitingKinds, type PowerFailure, type WaitingItem } from './power';

/** The broker and service codes of a refused power request, each with its own sentence. */
export const powerCodes = ['update_running', 'already_requested', 'busy', 'unavailable', 'job_failed', 'timeout', 'internal', 'bad_request'] as const;

const known = <T extends string>(list: readonly T[], v: string): v is T => (list as readonly string[]).includes(v);

/** The sentence for a failed POST /api/v1/power, and the field it belongs to (only the password can be wrong). */
export function powerErrorText(r: PowerFailure): { text: string; field: UpdateErrorField } {
  switch (r.kind) {
    case 'network':
      return { text: t.errorNetwork, field: null };
    case 'unavailable':
      return { text: t['pwrErr.unavailable'], field: null };
    case 'shape':
    case 'session':
      return { text: t.errorGeneric, field: null };
  }
  const key = `pwrErr.${r.code}`;
  if (known(powerCodes, r.code) && has(key)) return { text: msg(key), field: null };
  // wrong_password, rate_limited, the CSRF and Origin refusals, a body the service could not read.
  return actionErrorText(r.code, r.retryAfter);
}

/** One waiting action in words; a kind this version does not know gets the neutral sentence. */
export function waitingText(item: WaitingItem): string {
  if (!known(waitingKinds, item.kind)) return t['pwrWait.other'];
  if (item.kind === 'other_sessions' || item.kind === 'other_views') {
    const n = item.count ?? 1;
    return n === 1 ? msg(`pwrWait.${item.kind}.one`) : fmt(`pwrWait.${item.kind}.many`, { count: n });
  }
  return msg(`pwrWait.${item.kind}`);
}
