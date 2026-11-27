// The texts of the page Netzwerk (task N1, ORB-NET-001, ORB-NET-002 in part,
// docs/UI.md, "Network page"). Values come from the service and are text only.
import { fmt, has, msg, t } from './i18n';
import type { NetworkInterface, WifiSignal } from './metrics';

/**
 * The plain word for a signal level (a proposal, docs/UI.md): good from -60 dBm,
 * fair from -70 dBm, weak below. Without a level in dBm there is no word.
 */
export type SignalWord = 'good' | 'fair' | 'weak';

export function signalWord(levelDbm: number | null): SignalWord | null {
  if (levelDbm === null) return null;
  if (levelDbm >= -60) return 'good';
  if (levelDbm >= -70) return 'fair';
  return 'weak';
}

/** The signal as a value ("-52 dBm, Linkqualität 58") and its word as the note; null when nothing is known. */
export function signalText(s: WifiSignal | null | undefined): { value: string | null; note: string | null } {
  if (!s || (s.level_dbm === null && s.link_quality === null)) return { value: null, note: null };
  const word = signalWord(s.level_dbm);
  const note = word ? t[`netSignalWord.${word}`] : null;
  if (s.level_dbm !== null && s.link_quality !== null) return { value: fmt('netSignalValue', { level: s.level_dbm, quality: s.link_quality }), note };
  if (s.level_dbm !== null) return { value: fmt('netSignalLevel', { level: s.level_dbm }), note };
  return { value: fmt('netSignalQuality', { quality: s.link_quality ?? 0 }), note };
}

/** The addresses with prefix where the service sent them (task N1), else the plain ones. */
export function addressesOf(nic: NetworkInterface): string[] {
  const v4 = nic.ipv4_cidr && nic.ipv4_cidr.length === nic.ipv4.length ? nic.ipv4_cidr : nic.ipv4;
  const v6 = nic.ipv6_cidr && nic.ipv6_cidr.length === nic.ipv6.length ? nic.ipv6_cidr : nic.ipv6;
  return [...v4, ...v6];
}

export const linkLabel = (state: string) => msg(has('linkState.' + state) ? 'linkState.' + state : 'linkState.unknown');
export const kindLabel = (kind: string) => msg(has('netKind.' + kind) ? 'netKind.' + kind : 'netKind.other');
