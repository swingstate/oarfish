// Every foreground and background pair of the design tokens that the UI draws,
// with the WCAG 2.2 AA minimum for it. tokens.test.ts checks them in both
// themes; docs/UI.md prints the result. A new use of a colour pair belongs here.

export interface Pair {
  fg: string;
  bg: string;
  min: 4.5 | 3;
  /** Where the pair is used. */
  use: string;
}

const roles = ['success', 'information', 'warning', 'critical', 'neutral'] as const;
const surfaces = ['page', 'surface', 'surface-muted'] as const;

export const pairs: Pair[] = [
  // ---- Text, 4.5:1 ----
  ...surfaces.flatMap((s): Pair[] => [
    { fg: 'text', bg: s, min: 4.5, use: 'body text' },
    { fg: 'text-secondary', bg: s, min: 4.5, use: 'labels, small print, placeholders' },
    { fg: 'text-disabled', bg: s, min: 4.5, use: 'disabled button text' },
    { fg: 'accent', bg: s, min: 4.5, use: 'links' },
  ]),
  { fg: 'accent-fg', bg: 'accent', min: 4.5, use: 'skip link' },
  { fg: 'accent-subtle-fg', bg: 'accent-subtle-bg', min: 4.5, use: 'current navigation entry' },
  ...roles.flatMap((r): Pair[] => [
    { fg: `${r}-fg`, bg: `${r}-bg`, min: 4.5, use: `${r} badge, notice, alert` },
    { fg: `${r}-fg`, bg: 'surface', min: 4.5, use: `${r} text on a card` },
    { fg: 'text', bg: `${r}-bg`, min: 4.5, use: `${r} alert body text` },
    { fg: 'text-secondary', bg: `${r}-bg`, min: 4.5, use: `${r} notice secondary line` },
  ]),

  // ---- Graphics and UI components, 3:1 ----
  ...surfaces.map((s): Pair => ({ fg: 'border-strong', bg: s, min: 3, use: 'outline of meters, buttons, indicator' })),
  { fg: 'accent', bg: 'surface', min: 3, use: 'sparkline, normal meter fill' },
  { fg: 'accent', bg: 'surface-muted', min: 3, use: 'normal meter fill on its track' },
  { fg: 'warning-border', bg: 'surface-muted', min: 3, use: 'warning meter fill on its track' },
  { fg: 'critical-border', bg: 'surface-muted', min: 3, use: 'critical meter fill on its track' },
  ...(['page', 'surface', 'surface-muted', 'accent-subtle-bg'] as const).map(
    (s): Pair => ({ fg: 'focus-ring', bg: s, min: 3, use: 'focus ring' }),
  ),
  { fg: 'accent', bg: 'accent-subtle-bg', min: 3, use: 'current entry bar' },
  ...roles.flatMap((r): Pair[] => [
    { fg: `${r}-border`, bg: 'surface', min: 3, use: `${r} border and dot` },
    { fg: `${r}-border`, bg: `${r}-bg`, min: 3, use: `${r} border against its own fill` },
  ]),
];

/** Colours that are drawn dimmed when values are stale: they must still pass. */
export const stalePairs: Pair[] = [
  { fg: 'text', bg: 'surface', min: 4.5, use: 'stale value' },
  { fg: 'text-secondary', bg: 'surface', min: 4.5, use: 'stale label' },
  { fg: 'accent', bg: 'surface', min: 3, use: 'stale sparkline' },
  { fg: 'border-strong', bg: 'surface', min: 3, use: 'stale meter outline' },
];
