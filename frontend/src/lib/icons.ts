// Own icon set: 24 by 24 outline icons drawn as plain SVG paths, no icon
// library and no third-party licence. Icons are decorative; text carries the
// meaning (WCAG 1.4.1), so Icon.svelte always renders them aria-hidden.

export const iconPaths = {
  home: ['M3 11l9-8 9 8', 'M5 10v10h14V10', 'M10 20v-6h4v6'],
  device: ['M3 5h18a1 1 0 0 1 1 1v12a1 1 0 0 1-1 1H3a1 1 0 0 1-1-1V6a1 1 0 0 1 1-1z', 'M6 9h5', 'M6 13h12', 'M18 9h.01'],
  chip: [
    'M7 6h10a1 1 0 0 1 1 1v10a1 1 0 0 1-1 1H7a1 1 0 0 1-1-1V7a1 1 0 0 1 1-1z',
    'M10 10h4v4h-4z',
    'M9 3v3M15 3v3M9 18v3M15 18v3M3 9h3M3 15h3M18 9h3M18 15h3',
  ],
  memory: ['M3 7h18a1 1 0 0 1 1 1v8a1 1 0 0 1-1 1H3a1 1 0 0 1-1-1V8a1 1 0 0 1 1-1z', 'M7 17v3M12 17v3M17 17v3', 'M6 11h2M11 11h2M16 11h2'],
  thermometer: ['M14 14.8V5a2 2 0 0 0-4 0v9.8a4 4 0 1 0 4 0z'],
  drive: ['M3 14h18a1 1 0 0 1 1 1v4a1 1 0 0 1-1 1H3a1 1 0 0 1-1-1v-4a1 1 0 0 1 1-1z', 'M5 14l2.5-9h9L19 14', 'M6 17h.01M10 17h4'],
  network: ['M12 3a2 2 0 1 0 0 4 2 2 0 0 0 0-4z', 'M5 17a2 2 0 1 0 0 4 2 2 0 0 0 0-4z', 'M19 17a2 2 0 1 0 0 4 2 2 0 0 0 0-4z', 'M12 7v4M12 11l-6 6M12 11l6 6'],
  wifi: ['M5 12.55a11 11 0 0 1 14.08 0', 'M1.42 9a16 16 0 0 1 21.16 0', 'M8.53 16.11a6 6 0 0 1 6.95 0', 'M12 20h.01'],
  ethernet: ['M5 8h14a1 1 0 0 1 1 1v8a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V9a1 1 0 0 1 1-1z', 'M8 11v3M12 11v3M16 11v3', 'M12 18v3'],
  info: ['M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18z', 'M12 11v5', 'M12 8h.01'],
  warning: ['M10.3 3.9L2.4 17.5A2 2 0 0 0 4.1 20.5h15.8a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0z', 'M12 9.5v4', 'M12 17h.01'],
  critical: ['M8 2.5h8L21.5 8v8L16 21.5H8L2.5 16V8z', 'M12 7.5v5', 'M12 16h.01'],
  success: ['M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18z', 'M8 12.5l2.7 2.7L16 9.5'],
  clock: ['M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18z', 'M12 7v5l3 2'],
  user: ['M12 4a4 4 0 1 0 0 8 4 4 0 0 0 0-8z', 'M4 21a8 8 0 0 1 16 0'],
  lock: ['M6 11h12a1 1 0 0 1 1 1v8a1 1 0 0 1-1 1H6a1 1 0 0 1-1-1v-8a1 1 0 0 1 1-1z', 'M8 11V8a4 4 0 0 1 8 0v3', 'M12 15v2'],
} as const satisfies Record<string, readonly string[]>;

export type IconName = keyof typeof iconPaths;

/**
 * The icon for an interface kind from the API (`wifi`, `ethernet`, `other`).
 * A kind this version does not know gets the neutral generic icon, never a
 * guess from the name of the interface.
 */
export function kindIcon(kind: string): IconName {
  return kind === 'wifi' ? 'wifi' : kind === 'ethernet' ? 'ethernet' : 'network';
}
