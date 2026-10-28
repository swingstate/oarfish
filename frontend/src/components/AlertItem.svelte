<script lang="ts">
  import { fmt, has, msg, t } from '../lib/i18n';
  import type { Alert, AlertMessage } from '../lib/metrics';
  import { levelIcon, levelTone } from '../lib/ui';
  import Icon from './Icon.svelte';

  let { alert }: { alert: Alert } = $props();

  const text = (m: AlertMessage) => fmt(m.key, m.params);
  // The id is the rule, with the mount point after a colon for storage rules. A
  // rule without a short title simply has none; the cause then leads.
  const titleKey = $derived('alertTitle.' + alert.id.split(':')[0]);
</script>

<li class="alert" data-level={alert.level} data-alert-id={alert.id} data-tone={levelTone[alert.level]}>
  <span class="mark"><Icon name={levelIcon[alert.level]} /></span>
  <div class="main">
    {#if has(titleKey)}<h4>{msg(titleKey)}</h4>{/if}
    <dl>
      <div class="col">
        <dt>{t.alertCause}</dt>
        <dd class="cause">{text(alert.cause)}</dd>
      </div>
      <div class="col">
        <dt>{t.alertImpact}</dt>
        <dd>{text(alert.impact)}</dd>
      </div>
      <div class="col">
        <dt>{t.alertNext}</dt>
        <dd>{text(alert.next_step)}</dd>
      </div>
    </dl>
  </div>
</li>

<style>
  .alert {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-3);
    padding: var(--space-3) var(--space-4);
    background: var(--tone-bg);
    border: 1px solid var(--tone-border);
    border-inline-start-width: 4px;
    border-radius: var(--radius-md);
    --tone-bg: var(--color-information-bg);
    --tone-fg: var(--color-information-fg);
    --tone-border: var(--color-information-border);
  }

  .alert[data-tone='warning'] {
    --tone-bg: var(--color-warning-bg);
    --tone-fg: var(--color-warning-fg);
    --tone-border: var(--color-warning-border);
  }

  .alert[data-tone='critical'] {
    --tone-bg: var(--color-critical-bg);
    --tone-fg: var(--color-critical-fg);
    --tone-border: var(--color-critical-border);
  }

  .mark {
    color: var(--tone-fg);
    font-size: 1.25rem;
    line-height: 1;
    padding-top: 0.1em;
  }

  .main {
    flex: 1 1 12rem;
    min-width: 0;
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: var(--space-2);
  }

  h4 {
    font-size: var(--text-md);
    color: var(--tone-fg);
  }

  /* Cause, impact and next step side by side when there is room, stacked when not. */
  dl {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(min(100%, 14rem), 1fr));
    gap: var(--space-3) var(--space-5);
  }

  .col {
    min-width: 0;
  }

  dt {
    font-size: var(--text-xs);
    font-weight: var(--weight-semibold);
    letter-spacing: 0.04em;
    text-transform: uppercase;
    color: var(--tone-fg);
  }

  dd {
    color: var(--color-text);
    overflow-wrap: anywhere;
  }

  .cause {
    font-weight: var(--weight-medium);
  }

  @media (forced-colors: active) {
    .alert {
      border-color: CanvasText;
    }
  }
</style>
