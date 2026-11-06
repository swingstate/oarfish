import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';
import { render } from '@testing-library/svelte';
import BrandMark from './BrandMark.svelte';

const file = readFileSync(join(import.meta.dirname, '..', 'assets', 'oarfish-mark-mono.svg'), 'utf8');
const attrs = (el: Element) => ['d', 'fill', 'fill-rule', 'stroke', 'stroke-width', 'stroke-linecap'].map((a) => el.getAttribute(a));

describe('BrandMark', () => {
  it('draws the same paths as the owner file, in currentColor only', () => {
    const asset = new DOMParser().parseFromString(file, 'image/svg+xml').querySelectorAll('path');
    const { container } = render(BrandMark);
    const drawn = container.querySelectorAll('svg path');
    expect(drawn.length).toBe(3);
    expect(Array.from(drawn).map(attrs)).toEqual(Array.from(asset).map(attrs));
    expect(file).not.toMatch(/#[0-9a-fA-F]{3,8}\b|rgb|var\(/);
    expect(container.querySelector('svg')!.getAttribute('viewBox')).toBe('0 0 64 64');
  });

  it('is decorative: hidden from assistive technology, no title, no role', () => {
    const { container } = render(BrandMark);
    const svg = container.querySelector('svg')!;
    expect(svg.getAttribute('aria-hidden')).toBe('true');
    expect(svg.getAttribute('role')).toBeNull();
    expect(svg.getAttribute('aria-label')).toBeNull();
    expect(svg.querySelector('title')).toBeNull();
  });
});
