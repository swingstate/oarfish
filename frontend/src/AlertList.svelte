<script lang="ts">
  import { alertLevels, type Alert, type AlertMessage } from './lib/metrics';
  import { fmt, msg, t } from './lib/i18n';

  let { alerts }: { alerts: Alert[] } = $props();

  const text = (m: AlertMessage) => fmt(m.key, m.params);
</script>

<!--
  The only live region of the page (ORB-NFR-011): a change in the alerts is
  announced, the metric values that change every two seconds are not.
-->
<div class="alert-list" aria-live="polite" aria-relevant="additions text">
  {#if alerts.length === 0}
    <p class="muted">{t.alertsNone}</p>
  {:else}
    {#each alertLevels as level (level)}
      {@const items = alerts.filter((a) => a.level === level)}
      {#if items.length > 0}
        <div class="alert-group" data-level={level}>
          <h3>{msg('level.' + level)} <span class="count">({items.length})</span></h3>
          <ul>
            {#each items as alert (alert.id)}
              <li data-level={alert.level} data-alert-id={alert.id}>
                <dl>
                  <dt>{t.alertCause}</dt>
                  <dd><strong>{text(alert.cause)}</strong></dd>
                  <dt>{t.alertImpact}</dt>
                  <dd>{text(alert.impact)}</dd>
                  <dt>{t.alertNext}</dt>
                  <dd>{text(alert.next_step)}</dd>
                </dl>
              </li>
            {/each}
          </ul>
        </div>
      {/if}
    {/each}
  {/if}
</div>
