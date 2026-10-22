// German is the default UI language; English is prepared by adding a sibling
// dictionary with the same keys (ORB-GEN-006).
export const de = {
  tagline: 'Verwaltung für ARM-Einzelplatinencomputer und Embedded Linux',
  statusLabel: 'Orbit-Dienst',
  checking: 'Wird geprüft …',
  ok: 'Läuft',
  unreachable: 'Nicht erreichbar',
  recheck: 'Erneut prüfen',
  foundation: 'Repository-Grundgerüst (Gate G1). Es sind noch keine Verwaltungsfunktionen enthalten.',
} as const;

export type Messages = typeof de;
export const t: Messages = de;
