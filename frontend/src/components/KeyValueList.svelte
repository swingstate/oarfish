<script lang="ts">
  import { t } from '../lib/i18n';

  import type { KeyValueItem } from '../lib/ui';

  let { items }: { items: KeyValueItem[] } = $props();
</script>

<!--
  Label and value never share a fixed two-column grid. They sit in one wrapping
  row: side by side when they fit, the value below the label when they do not.
  So a narrow card or a long German word cannot squeeze either into a thin column.
-->
<dl class="kv">
  {#each items as item}
    <div class="row">
      <dt>{item.label}</dt>
      <dd class:num={item.mono} data-testid={item.testid} title={item.title}>
        {#if item.value === null}
          <span class="placeholder">{item.placeholder ?? t.unavailableValue}</span>
        {:else}
          <span class="value">{item.value}</span>
        {/if}
        {#if item.note}{' '}<span class="note">({item.note})</span>{/if}
      </dd>
    </div>
  {/each}
</dl>

<style>
  .kv {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: var(--space-2);
    min-width: 0;
  }

  .row {
    display: flex;
    flex-wrap: wrap;
    justify-content: space-between;
    column-gap: var(--space-4);
    row-gap: 0;
    min-width: 0;
  }

  dt {
    max-width: 100%;
    color: var(--color-text-secondary);
    overflow-wrap: anywhere;
  }

  dd {
    max-width: 100%;
    min-width: 0;
    text-align: end;
    overflow-wrap: anywhere;
  }

  .note,
  .placeholder {
    color: var(--color-text-secondary);
  }
</style>
