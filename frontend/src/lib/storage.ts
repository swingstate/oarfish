// The marks and texts of the page Speicher (task ST1, ORB-STO-001 to -004 in
// part, docs/UI.md, "Storage page"). Read-only: nothing here changes a medium.
import { fmt, has, msg, t } from './i18n';
import type { Storage } from './metrics';
import type { Tone } from './ui';

// The thresholds of the existing low-capacity alert (backend/internal/metrics/alerts.go,
// docs/DASHBOARD.md): above 85 % relevant, above 95 % critical, real file systems only.
export const storageRelevantPercent = 85;
export const storageCriticalPercent = 95;

export type CapacityLevel = 'ok' | 'relevant' | 'critical';

export function capacityLevel(usedPercent: number): CapacityLevel {
  if (usedPercent > storageCriticalPercent) return 'critical';
  if (usedPercent > storageRelevantPercent) return 'relevant';
  return 'ok';
}

/** The sentence of the low-capacity warning, the same as the alert's; null below the threshold. */
export function capacityWarning(s: Storage): { tone: Tone; text: string } | null {
  const level = capacityLevel(s.used_percent);
  if (level === 'ok') return null;
  const threshold = level === 'critical' ? storageCriticalPercent : storageRelevantPercent;
  const p = { mount: s.mount_point, percent: Math.round(s.used_percent), threshold };
  return {
    tone: level === 'critical' ? 'critical' : 'warning',
    text: `${fmt(`alert.storage_usage.${level}.cause`, p)} ${msg(`alert.storage_usage.${level}.impact`)}`,
  };
}

/** The disk the root file system is on, or '' when unknown. */
export function rootDisk(list: Storage[]): string {
  return list.find((s) => s.mount_point === '/')?.disk ?? '';
}

/**
 * The marks of a real file system (ORB-STO-003): on the boot medium (never
 * offered as removable), a removable or USB medium.
 */
export function storageMarks(s: Storage, root: string): string[] {
  const disk = s.disk ?? '';
  if (disk !== '' && disk === root) return [t.stoMarkBoot];
  if (s.removable === true || s.usb === true) return [t.stoMarkRemovable];
  return [];
}

/** The device of a real file system: the kernel name, or Unbekannt. */
export function deviceText(s: Storage): string {
  return s.device ? s.device : t.unknownValue;
}

/** An other mount is shown by its kind, never by its source. */
export const mountKindText = (kind: string) => msg(has('stoKind.' + kind) ? 'stoKind.' + kind : 'stoKind.other');
export const mountKindNote = (kind: string) => msg(has('stoKindNote.' + kind) ? 'stoKindNote.' + kind : 'stoKindNote.other');
