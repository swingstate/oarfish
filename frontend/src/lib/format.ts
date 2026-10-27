import { fmt } from './i18n';

const one = new Intl.NumberFormat('de-DE', { minimumFractionDigits: 1, maximumFractionDigits: 1 });
const whole = new Intl.NumberFormat('de-DE', { maximumFractionDigits: 0 });
const clock = new Intl.DateTimeFormat('de-DE', { hour: '2-digit', minute: '2-digit', second: '2-digit' });

const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB'] as const;

/** Bytes in binary units with one decimal, for example "1,5 GiB". */
export function formatBytes(bytes: number | null): string | null {
  if (bytes === null) return null;
  let value = bytes;
  let i = 0;
  while (value >= 1024 && i < units.length - 1) {
    value /= 1024;
    i++;
  }
  return `${i === 0 ? whole.format(value) : one.format(value)} ${units[i]}`;
}

/** Bytes per second, for example "12,3 KiB/s". */
export function formatRate(bytesPerSecond: number | null): string | null {
  const b = formatBytes(bytesPerSecond);
  return b === null ? null : `${b}/s`;
}

export function formatPercent(value: number | null): string | null {
  return value === null ? null : `${whole.format(value)} %`;
}

export function formatCelsius(value: number | null): string | null {
  return value === null ? null : `${one.format(value)} °C`;
}

export function formatMHz(value: number | null): string | null {
  return value === null ? null : `${whole.format(value)} MHz`;
}

export function formatLoad(value: number): string {
  return new Intl.NumberFormat('de-DE', { minimumFractionDigits: 2, maximumFractionDigits: 2 }).format(value);
}

/** Local time of an ISO timestamp, or null when it is not one. */
export function formatClock(iso: string): string | null {
  const t = Date.parse(iso);
  return Number.isNaN(t) ? null : clock.format(new Date(t));
}

/** Uptime as days and hours, hours and minutes, or minutes. */
export function formatUptime(seconds: number | null): string | null {
  if (seconds === null) return null;
  const total = Math.floor(seconds / 60);
  const days = Math.floor(total / 1440);
  const hours = Math.floor((total % 1440) / 60);
  const minutes = total % 60;
  if (days > 0) return fmt('uptimeDaysHours', { days, hours });
  if (hours > 0) return fmt('uptimeHoursMinutes', { hours, minutes });
  return fmt('uptimeMinutes', { minutes });
}

/** Link speed in megabits per second. */
export function formatSpeed(mbps: number | null): string | null {
  return mbps === null ? null : `${whole.format(mbps)} Mbit/s`;
}
