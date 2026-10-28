// Small shared definitions of the UI: tones, and how API vocabulary maps to
// them. The mapping is keyed by the API's own enumerations (support tier, alert
// level, link state), never by an interface, port or board name (D-C43).
import type { IconName } from './icons';
import type { AlertLevel } from './metrics';

/** The five looks of a badge or notice. Only the last four carry a meaning. */
export type Tone = 'neutral' | 'success' | 'information' | 'warning' | 'critical';

export const toneIcon: Record<Tone, IconName> = {
  neutral: 'info',
  success: 'success',
  information: 'info',
  warning: 'warning',
  critical: 'critical',
};

export const levelTone: Record<AlertLevel, Tone> = {
  critical: 'critical',
  relevant: 'warning',
  informational: 'information',
};

export const levelIcon: Record<AlertLevel, IconName> = {
  critical: 'critical',
  relevant: 'warning',
  informational: 'info',
};

/** How full a usage bar is, by the same steps as the storage alerts. */
export type MeterLevel = 'ok' | 'relevant' | 'critical';

/** Support tier to tone. Unknown tiers are neutral, never alarming. */
export function tierTone(tier: string | null | undefined): Tone {
  switch (tier) {
    case 'verified':
      return 'success';
    case 'compatible':
      return 'information';
    case 'experimental':
      return 'warning';
    case 'unsupported':
      return 'critical';
    default:
      return 'neutral'; // community, generic, or a tier this version does not know
  }
}

/** A link is either up (success) or it is not. "No link" and "unknown" are neutral, not a warning. */
export function linkTone(state: string): Tone {
  return state === 'up' ? 'success' : 'neutral';
}

/** Range of the temperature sparkline: a few degrees around what was seen. */
export function seriesRange(values: (number | null)[], fallback: { min: number; max: number }): { min: number; max: number } {
  const known = values.filter((v): v is number => v !== null);
  if (known.length === 0) return fallback;
  const lo = Math.floor(Math.min(...known)) - 2;
  return { min: lo, max: Math.max(Math.ceil(Math.max(...known)) + 2, lo + 10) };
}

/** One label and value of a KeyValueList. */
export interface KeyValueItem {
  label: string;
  /** Null shows the placeholder, in a neutral look. */
  value: string | null;
  /** Shown in parentheses after the value, in the secondary colour. */
  note?: string | null;
  /** Monospace face with equal-width digits, for numbers. */
  mono?: boolean;
  /** Text instead of the value when it is missing. Default: "Nicht verfügbar". */
  placeholder?: string;
  /** Full text for a long value that is cut off. */
  title?: string;
  testid?: string;
}

/** One entry of the SummaryStrip: a label with a text, or a badge, and optionally a link. */
export interface SummaryItem {
  label: string;
  value?: string | null;
  note?: string | null;
  placeholder?: string;
  /** Long values are cut to one line; the full text is the title. */
  truncate?: boolean;
  mono?: boolean;
  testid?: string;
  badge?: { text: string; tone: Tone; testid?: string };
  link?: { href: string; text: string };
}

/** One entry of the navigation in the app shell. */
export interface NavItem {
  id: string;
  href: string;
  label: string;
  icon: IconName;
}
