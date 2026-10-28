<script lang="ts">
  import { t } from '../lib/i18n';
  import type { SummaryItem } from '../lib/ui';
  import Badge from './Badge.svelte';

  interface Props {
    items: SummaryItem[];
    /** Accessible name of the strip. */
    label: string;
    stale?: boolean;
  }
  let { items, label, stale = false }: Props = $props();
</script>

<!-- A plain group, not a section: the alerts stay the first section of the page. -->
<div class="strip" role="group" aria-label={label} data-stale={stale}>
  <dl>
    {#each items as item}
      <div class="item">
        <dt>{item.label}</dt>
        <dd>
          {#if item.badge}
            <Badge tone={item.badge.tone} data-testid={item.badge.testid}>{item.badge.text}</Badge>
          {:else if item.value === null || item.value === undefined}
            <span class="placeholder" data-testid={item.testid}>{item.placeholder ?? t.unknownValue}</span>
          {:else}
            <span
              class="value"
              class:truncate={item.truncate}
              class:num={item.mono}
              data-testid={item.testid}
              title={item.truncate ? item.value : undefined}>{item.value}</span
            >
          {/if}
          {#if item.note}{' '}<span class="note">({item.note})</span>{/if}
          {#if item.link}<a href={item.link.href}>{item.link.text}</a>{/if}
        </dd>
      </div>
    {/each}
  </dl>
  {#if stale}<div class="stale"><Badge tone="warning" icon="clock">{t.staleBadge}</Badge></div>{/if}
</div>

<style>
  .strip {
    padding: var(--space-3) var(--space-4);
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-card);
  }

  dl {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(min(100%, 9.5rem), 1fr));
    gap: var(--space-3) var(--space-5);
  }

  .item {
    min-width: 0;
  }

  dt {
    font-size: var(--text-xs);
    font-weight: var(--weight-semibold);
    letter-spacing: 0.04em;
    text-transform: uppercase;
    color: var(--color-text-secondary);
  }

  dd {
    min-width: 0;
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    column-gap: var(--space-2);
    font-weight: var(--weight-medium);
  }

  .value {
    max-width: 100%;
    overflow-wrap: anywhere;
  }

  .note,
  .placeholder {
    font-weight: var(--weight-regular);
    color: var(--color-text-secondary);
  }

  a {
    font-size: var(--text-sm);
    font-weight: var(--weight-regular);
    /* A link of this size still has a 24 px high target. */
    display: inline-flex;
    align-items: center;
    min-height: var(--target-min);
  }

  .stale {
    display: flex;
    justify-content: flex-end;
    margin-top: var(--space-2);
  }

  .strip[data-stale='true'] dl {
    opacity: var(--opacity-stale);
  }

  @media (forced-colors: active) {
    .strip {
      border-color: CanvasText;
    }
  }
</style>
