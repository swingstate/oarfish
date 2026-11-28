<script lang="ts">
  import { onMount } from 'svelte';
  import Badge from '../components/Badge.svelte';
  import Card from '../components/Card.svelte';
  import ConnectionIndicator from '../components/ConnectionIndicator.svelte';
  import EmptyState from '../components/EmptyState.svelte';
  import Icon from '../components/Icon.svelte';
  import KeyValueList from '../components/KeyValueList.svelte';
  import Notice from '../components/Notice.svelte';
  import PageHeader from '../components/PageHeader.svelte';
  import * as f from '../lib/format';
  import { kindIcon } from '../lib/icons';
  import { fmt, t } from '../lib/i18n';
  import { LiveSnapshot } from '../lib/live.svelte';
  import type { NetworkInterface } from '../lib/metrics';
  import { addressesOf, hiddenNote, kindLabel, linkLabel, signalText } from '../lib/network';
  import { routingItems } from '../lib/routing';
  import { linkTone, type KeyValueItem } from '../lib/ui';

  // Task N1 (ORB-NET-001, ORB-NET-002 in part; docs/UI.md, "Network page"). The
  // live data path of the Dashboard: sampled by the service only while a page
  // watches. No SSID (it needs netlink, ADR 0008 S1.5), and no hardware address
  // anywhere (decision 9); hidden addresses are only counted (task SF1).
  // Nothing on this page changes the device.
  const live = new LiveSnapshot();
  onMount(() => live.start());

  const s = $derived(live.snapshot);
  const lastUpdate = $derived(s ? f.formatClock(s.collected_at) : null);
  const measuring = $derived(live.conn === 'live' && !live.afterPause);

  function facts(nic: NetworkInterface): KeyValueItem[] {
    const rate = live.rates[nic.name];
    const rateText = (bytes: number | null | undefined) => f.formatRate(bytes ?? null) ?? (measuring ? t.netRateWaiting : t.unavailableValue);
    const items: KeyValueItem[] = [
      { label: t.netKind, value: kindLabel(nic.kind) },
      { label: t.netLinkState, value: linkLabel(nic.state) },
      { label: t.netSpeed, value: f.formatSpeed(nic.speed_mbps), mono: true },
    ];
    if (nic.kind === 'wifi') {
      const sig = signalText(nic.signal);
      items.push({ label: t.netSignal, value: sig.value, note: sig.note, mono: true, testid: 'signal-' + nic.name });
    }
    items.push(
      { label: t.netRx, value: rateText(rate?.rx), mono: true, testid: 'rx-' + nic.name },
      { label: t.netTx, value: rateText(rate?.tx), mono: true, testid: 'tx-' + nic.name },
      {
        label: t.netTotals,
        value: `${t.netRx} ${f.formatBytes(nic.rx_bytes) ?? t.unavailableValue}, ${t.netTx} ${f.formatBytes(nic.tx_bytes) ?? t.unavailableValue}`,
        mono: true,
      },
    );
    return items;
  }
</script>

<PageHeader title={t.networkTitle}>
  {#snippet aside()}<ConnectionIndicator state={live.conn} />{/snippet}
</PageHeader>

<div data-testid="network-page">
  {#if s === null}
    {#if live.loadFailed && (live.conn === 'reconnecting' || live.conn === 'stale')}
      <EmptyState icon="warning" tone="warning" title={t.netPageLoadError} description={t.netPageLoadErrorHint} role="alert" data-state="load-error">
        <button type="button" onclick={() => void live.load()}>{t.dashboardRetry}</button>
      </EmptyState>
    {:else}
      <EmptyState icon="clock" title={t.netPageLoading} role="status" data-state="loading" />
    {/if}
  {:else}
    <div class="stack">
      {#if live.troubled}
        <Notice tone="warning" data-state={live.conn} data-testid="connection-notice">
          <p>{live.conn === 'reconnecting' ? t.connLost : t.connStale}</p>
          {#if lastUpdate}<p class="secondary">{fmt('connLastUpdate', { time: lastUpdate })}</p>{/if}
        </Notice>
      {/if}

      <div class="card-grid">
        <Card id="interfaces" title={t.netPageInterfaces} icon="network" stale={live.stale} wide data-testid="interfaces">
          {#if s.network.length === 0}
            <EmptyState compact icon="info" title={t.netNone} titleTestid="no-network" />
          {:else}
            <ul class="rows">
              {#each s.network as nic (nic.name)}
                {@const hidden = hiddenNote(nic)}
                <li class="stack-sm" data-testid="iface-{nic.name}">
                  <div class="line">
                    <h3 class="nic">
                      <Icon name={kindIcon(nic.kind)} />
                      <span class="num truncate" title={nic.name}>{nic.name}</span>
                    </h3>
                    <Badge tone={linkTone(nic.state)}>{linkLabel(nic.state)}</Badge>
                  </div>
                  <section class="stack-sm" aria-label="{t.netAddresses} {nic.name}">
                    <p class="secondary caption">{t.netAddresses}</p>
                    <ul class="addresses" data-testid="addresses-{nic.name}">
                      {#each addressesOf(nic) as address (address)}
                        <li class="address num">{address}</li>
                      {:else}
                        <li class="secondary">{t.netNoAddress}</li>
                      {/each}
                    </ul>
                    {#if hidden}<p class="secondary caption" data-testid="hidden-{nic.name}">{hidden}</p>{/if}
                  </section>
                  <KeyValueList items={facts(nic)} />
                </li>
              {/each}
            </ul>
          {/if}
        </Card>

        {#if s.routing}
          <Card id="routing" title={t.netRoutingHeading} icon="network" stale={live.stale}>
            <KeyValueList items={routingItems(s.routing)} />
          </Card>
        {/if}
      </div>
    </div>
  {/if}
</div>

<style>
  .rows {
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .stack-sm {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: var(--space-2);
    min-width: 0;
  }
  .secondary {
    color: var(--color-text-secondary);
  }
  .nic :global(.icon) {
    color: var(--color-text-secondary);
  }
  .line {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-2);
    min-width: 0;
  }
  .nic {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    min-width: 0;
    margin: 0;
    font-size: var(--text-md);
    font-weight: var(--weight-semibold);
  }
  .addresses {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-1) var(--space-3);
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .address {
    overflow-wrap: anywhere;
  }
  .caption {
    margin: 0;
    font-size: var(--text-sm);
  }
</style>
