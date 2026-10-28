<script lang="ts">
  import { t } from '../lib/i18n';
  import type { StreamState } from '../lib/stream';

  let { state }: { state: StreamState } = $props();

  const label = $derived(
    {
      live: t.connIndicatorLive,
      connecting: t.connIndicatorConnecting,
      reconnecting: t.connIndicatorReconnecting,
      stale: t.connIndicatorStale,
      paused: t.connIndicatorPaused,
    }[state],
  );
</script>

<!-- Not a live region: only the alert list is announced (ORB-NFR-011). The text always says the state. -->
<p class="conn" data-state={state} data-testid="connection-indicator">
  <span class="dot" aria-hidden="true"></span>
  {label}
</p>

<style>
  .conn {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    min-height: 1.75rem;
    padding: 0 var(--space-3);
    font-size: var(--text-sm);
    font-weight: var(--weight-medium);
    color: var(--color-text);
    background: var(--color-surface);
    border: 1px solid var(--color-border-strong);
    border-radius: var(--radius-full);
  }

  .dot {
    flex: none;
    width: 0.5rem;
    height: 0.5rem;
    border-radius: var(--radius-full);
    background: var(--color-neutral-border);
  }

  .conn[data-state='live'] .dot {
    background: var(--color-success-border);
  }

  .conn[data-state='reconnecting'] .dot,
  .conn[data-state='stale'] .dot {
    background: var(--color-warning-border);
  }

  /* Waiting for the first value: a hollow dot, so the shape differs from "live". */
  .conn[data-state='connecting'] .dot,
  .conn[data-state='paused'] .dot {
    background: transparent;
    box-shadow: inset 0 0 0 2px var(--color-neutral-border);
  }

  @media (forced-colors: active) {
    .dot {
      forced-color-adjust: none;
      background: CanvasText;
    }

    .conn[data-state='connecting'] .dot,
    .conn[data-state='paused'] .dot {
      background: transparent;
      box-shadow: inset 0 0 0 2px CanvasText;
    }
  }
</style>
