<script lang="ts">
  import type { Snippet } from 'svelte';
  import type { HTMLAttributes } from 'svelte/elements';
  import type { IconName } from '../lib/icons';
  import { t } from '../lib/i18n';
  import Badge from './Badge.svelte';
  import Icon from './Icon.svelte';

  interface Props extends HTMLAttributes<HTMLElement> {
    /** Short id; the heading gets `<id>-heading` and names the section. */
    id: string;
    title: string;
    icon?: IconName;
    /** The values are old (connection lost): they are dimmed and the card says so. */
    stale?: boolean;
    /** Spans every column of the card grid. */
    wide?: boolean;
    children: Snippet;
  }
  let { id, title, icon, stale = false, wide = false, children, ...rest }: Props = $props();
</script>

<section class="card" class:wide data-stale={stale} aria-labelledby="{id}-heading" {...rest}>
  <div class="head">
    <h2 id="{id}-heading">{#if icon}<Icon name={icon} />{/if}<span>{title}</span></h2>
    {#if stale}<Badge tone="warning" icon="clock">{t.staleBadge}</Badge>{/if}
  </div>
  <div class="body">{@render children()}</div>
</section>

<style>
  .card {
    min-width: 0;
    padding: var(--space-4);
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-card);
  }

  .wide {
    grid-column: 1 / -1;
  }

  .head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-2) var(--space-3);
    margin-bottom: var(--space-3);
  }

  h2 {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    min-width: 0;
    font-size: var(--text-lg);
    color: var(--color-text);
  }

  h2 :global(.icon) {
    color: var(--color-text-secondary);
  }

  .body {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: var(--space-3);
    min-width: 0;
  }

  /* Old values stay readable: the dimming keeps every text above 4.5:1 (tokens.test.ts). */
  .card[data-stale='true'] .body {
    opacity: var(--opacity-stale);
  }

  @media (forced-colors: active) {
    .card {
      border-color: CanvasText;
    }
  }
</style>
