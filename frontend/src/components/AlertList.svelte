<script lang="ts">
  import { alertLevels, type Alert } from '../lib/metrics';
  import { levelTone } from '../lib/ui';
  import { msg, t } from '../lib/i18n';
  import AlertItem from './AlertItem.svelte';
  import Icon from './Icon.svelte';

  interface Props {
    alerts: Alert[];
    /** The values are old: an empty list then only says what was true at the last reading. */
    stale?: boolean;
  }
  let { alerts, stale = false }: Props = $props();
</script>

<!--
  The only live region of the page (ORB-NFR-011): a change in the alerts is
  announced, the metric values that change every two seconds are not.
-->
<div class="alert-list" aria-live="polite" aria-relevant="additions text">
  {#if alerts.length === 0}
    <!-- Old values: no all-clear mark. A neutral clock says "as of the last reading". -->
    <p class="none" class:old={stale}><Icon name={stale ? 'clock' : 'success'} /><span>{stale ? t.alertsNoneStale : t.alertsNone}</span></p>
  {:else}
    {#each alertLevels as level (level)}
      {@const items = alerts.filter((a) => a.level === level)}
      {#if items.length > 0}
        <div class="alert-group" data-level={level} data-tone={levelTone[level]}>
          <h3>{msg('level.' + level)} <span class="count">({items.length})</span></h3>
          <ul>
            {#each items as alert (alert.id)}
              <AlertItem {alert} />
            {/each}
          </ul>
        </div>
      {/if}
    {/each}
  {/if}
</div>

<style>
  .alert-list {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: var(--space-4);
  }

  .none {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    color: var(--color-success-fg);
  }

  .none :global(.icon) {
    color: var(--color-success-border);
  }

  .none.old {
    color: var(--color-text-secondary);
  }

  .none.old :global(.icon) {
    color: var(--color-text-secondary);
  }

  h3 {
    margin-bottom: var(--space-2);
    font-size: var(--text-sm);
    letter-spacing: 0.04em;
    text-transform: uppercase;
    color: var(--color-text-secondary);
  }

  .count {
    font-weight: var(--weight-regular);
  }

  ul {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: var(--space-2);
  }
</style>
