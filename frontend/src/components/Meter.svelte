<script lang="ts">
  import type { MeterLevel } from '../lib/ui';

  interface Props {
    /** 0 to 100. Unknown shows an empty bar; the text next to it says "not available". */
    value: number | null;
    /** Accessible name: what is measured, for example "Auslastung". */
    label: string;
    level?: MeterLevel;
  }
  let { value, label, level = 'ok' }: Props = $props();
</script>

<!-- A native progress element: name and value reach assistive technology without ARIA. -->
<progress class="meter" max="100" value={value ?? 0} data-level={level} aria-label={label}></progress>

<style>
  .meter {
    display: block;
    width: 100%;
    height: 0.625rem;
    padding: 0;
    appearance: none;
    -webkit-appearance: none;
    color: var(--meter-fill);
    background: var(--color-surface-muted);
    border: 1px solid var(--color-border-strong);
    border-radius: var(--radius-sm);
    overflow: hidden;
    --meter-fill: var(--color-accent);
  }

  /* The fill does not ease to a new width: that would be motion on every update. */
  .meter::-webkit-progress-bar {
    background: var(--color-surface-muted);
  }

  .meter::-webkit-progress-value {
    background: var(--meter-fill);
  }

  .meter::-moz-progress-bar {
    background: var(--meter-fill);
  }

  .meter[data-level='relevant'] {
    --meter-fill: var(--color-warning-border);
  }

  .meter[data-level='critical'] {
    --meter-fill: var(--color-critical-border);
  }

  @media (forced-colors: active) {
    .meter {
      border-color: CanvasText;
      --meter-fill: Highlight;
    }
  }
</style>
