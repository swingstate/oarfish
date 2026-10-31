<script lang="ts">
  import { onMount } from 'svelte';
  import AlertList from '../components/AlertList.svelte';
  import Badge from '../components/Badge.svelte';
  import Card from '../components/Card.svelte';
  import ConnectionIndicator from '../components/ConnectionIndicator.svelte';
  import EmptyState from '../components/EmptyState.svelte';
  import Icon from '../components/Icon.svelte';
  import KeyValueList from '../components/KeyValueList.svelte';
  import Meter from '../components/Meter.svelte';
  import Notice from '../components/Notice.svelte';
  import PageHeader from '../components/PageHeader.svelte';
  import Sparkline from '../components/Sparkline.svelte';
  import SummaryStrip from '../components/SummaryStrip.svelte';
  import * as f from '../lib/format';
  import { kindIcon } from '../lib/icons';
  import { fmt, has, msg, t } from '../lib/i18n';
  import {
    computeRates,
    cpuTemperature,
    fetchMetrics,
    memoryUsedPercent,
    pushPoint,
    type Alert,
    type DeviceSummary,
    type History,
    type Rate,
    type Snapshot,
  } from '../lib/metrics';
  import { fetchSession, sessionLost } from '../lib/session';
  import { MetricsStream, type StreamState } from '../lib/stream';
  import { linkTone, seriesRange, tierTone, type KeyValueItem, type MeterLevel, type SummaryItem } from '../lib/ui';

  let snapshot = $state<Snapshot | null>(null);
  let device = $state<DeviceSummary | null>(null);
  let alerts = $state<Alert[]>([]);
  let cpuSeries = $state<(number | null)[]>([]);
  let memSeries = $state<(number | null)[]>([]);
  let tempSeries = $state<(number | null)[]>([]);
  let rates = $state<Record<string, Rate>>({});
  let conn = $state<StreamState>('connecting');
  let loadFailed = $state(false);

  // Not reactive: bookkeeping that decides which data wins.
  let lastAt = 0;
  let alertsFromStream = false;
  let historyFromStream = false;
  let loadingDevice = false;

  const stale = $derived(snapshot !== null && (conn === 'stale' || conn === 'reconnecting'));
  // A statement about the present ("no alerts", "measuring") is only made while the stream is live.
  const live = $derived(conn === 'live');

  function applySnapshot(s: Snapshot, fromStream: boolean): void {
    const at = Date.parse(s.collected_at);
    if (at <= lastAt) return; // an older reading must not replace a newer one
    lastAt = at;
    rates = computeRates(snapshot, s);
    snapshot = s;
    if (fromStream) {
      cpuSeries = pushPoint(cpuSeries, s.cpu.usage_percent);
      memSeries = pushPoint(memSeries, memoryUsedPercent(s));
      tempSeries = pushPoint(tempSeries, cpuTemperature(s));
    }
  }

  function setHistory(h: History): void {
    cpuSeries = h.cpu_percent.slice(-60);
    memSeries = h.memory_used_percent.slice(-60);
    tempSeries = h.temperature_celsius.slice(-60);
  }

  async function load(): Promise<void> {
    if (loadingDevice) return;
    loadingDevice = true;
    loadFailed = false;
    const result = await fetchMetrics();
    loadingDevice = false;
    if (!result.ok) {
      loadFailed = true;
      return;
    }
    const m = result.metrics;
    device = m.device;
    applySnapshot(m, false);
    if (!alertsFromStream) alerts = m.alerts;
    if (!historyFromStream) setHistory(m.history);
  }

  onMount(() => {
    const stream = new MetricsStream({
      onSnapshot: (s) => applySnapshot(s, true),
      onAlerts: (a) => {
        alertsFromStream = true;
        alerts = a;
      },
      onHistory: (h) => {
        historyFromStream = true;
        setHistory(h);
      },
      onState: (s) => {
        conn = s;
        // The device summary only comes with the one-shot request; try again if it failed.
        if (s === 'live' && device === null) void load();
      },
      // The server ended the stream because the session ended (logout elsewhere,
      // expiry, a password change): back to the login page, no reconnect.
      onSessionEnded: () => sessionLost('expired'),
    }, {
      // Before reconnecting by hand after a refused stream: is the session still
      // there? A network error counts as yes (unstable Wi-Fi, ORB-NFR-014).
      checkSession: async () => {
        const r = await fetchSession();
        if (!r.ok || r.session.state === 'authenticated') return true;
        sessionLost(r.session.state === 'setup_required' ? 'setup_required' : 'expired');
        return false;
      },
    });
    stream.start();
    void load();
    return () => stream.stop();
  });

  const or = (value: string | null | undefined, fallback: string = t.unknownValue) => value ?? fallback;
  const lastUpdate = $derived(snapshot ? f.formatClock(snapshot.collected_at) : null);

  const tempRange = $derived(seriesRange(tempSeries, { min: 20, max: 80 }));

  const lastOf = (series: (number | null)[]) => series.findLast((v) => v !== null) ?? null;
  const barLevel = (percent: number): MeterLevel => (percent > 95 ? 'critical' : percent > 85 ? 'relevant' : 'ok');

  // Texts for values the API names by key. A value this version does not know
  // gets the neutral wording, never a raw key and never a guess.
  const linkLabel = (state: string) => msg(has('linkState.' + state) ? 'linkState.' + state : 'linkState.unknown');
  const kindLabel = (kind: string) => msg(has('netKind.' + kind) ? 'netKind.' + kind : 'netKind.other');

  const cpuFrequency = (s: Snapshot): string | null => {
    const { current_mhz: now, max_mhz: max } = s.cpu.frequency;
    if (now === null) return null;
    if (max === null) return f.formatMHz(now);
    return fmt('cpuFreqOfMax', { current: f.formatMHz(now) ?? '', max: f.formatMHz(max) ?? '' });
  };

  const summary = (s: Snapshot, d: DeviceSummary | null): SummaryItem[] => [
    { label: t.sumHost, value: s.hostname, truncate: true, mono: true, testid: 'hostname' },
    { label: t.sumBoard, value: d?.model ?? null, note: d?.soc_family ?? null },
    { label: t.sumOs, value: d?.os_pretty_name ?? null },
    { label: t.sumUptime, value: f.formatUptime(s.uptime_seconds), mono: true, testid: 'uptime' },
    {
      label: t.sumSupport,
      ...(d?.support_tier
        ? { badge: { text: msg('tier.' + d.support_tier), tone: tierTone(d.support_tier), testid: 'tier' } }
        : { value: null }),
      link: { href: '#/device', text: t.sumDeviceLink },
    },
  ];
</script>

<PageHeader title={t.homeTitle}>
  {#snippet aside()}<ConnectionIndicator state={conn} />{/snippet}
</PageHeader>

{#if snapshot === null}
  {#if loadFailed && (conn === 'reconnecting' || conn === 'stale')}
    <EmptyState icon="warning" tone="warning" title={t.homeLoadError} description={t.homeLoadErrorHint} data-state="unreachable">
      <button type="button" onclick={load}>{t.homeRetry}</button>
    </EmptyState>
  {:else}
    <EmptyState icon="clock" title={conn === 'connecting' ? t.homeLoading : t.connConnecting} titleTestid="home-loading" />
  {/if}
{:else}
  {@const s = snapshot}
  <div class="stack">
    {#if stale}
      <Notice tone="warning" data-state={conn} data-testid="connection-notice">
        <p>{conn === 'reconnecting' ? t.connLost : t.connStale}</p>
        {#if lastUpdate}<p class="secondary">{fmt('connLastUpdate', { time: lastUpdate })}</p>{/if}
      </Notice>
    {/if}

    <div class="stack home" data-stale={stale} data-testid="home">
      <SummaryStrip items={summary(s, device)} label={t.summaryHeading} {stale} />

      <Card id="alerts" title={t.alertsHeading} wide>
        <AlertList {alerts} stale={!live} />
      </Card>

      <div class="card-grid">
        <Card id="cpu" title={t.cpuHeading} icon="chip" {stale}>
          <p class="metric num" class:missing={s.cpu.usage_percent === null} data-testid="cpu-usage">
            {or(f.formatPercent(s.cpu.usage_percent), t.unavailableValue)}
          </p>
          <Meter value={s.cpu.usage_percent} label={t.cpuUsage} />
          <Sparkline
            values={cpuSeries}
            min={0}
            max={100}
            label={fmt('sparkCpu', { value: or(f.formatPercent(lastOf(cpuSeries)), t.unavailableValue) })}
          />
          <KeyValueList
            items={[
              { label: t.cpuLoad, value: s.cpu.load ? f.formatLoad(s.cpu.load.one) : null, mono: true },
              { label: t.cpuFreq, value: cpuFrequency(s), mono: true, testid: 'cpu-frequency' },
            ]}
          />
          <details>
            <summary>{t.technicalDetails}</summary>
            <div class="details-body">
              <KeyValueList
                items={[
                  { label: t.cpuGovernor, value: s.cpu.governor },
                  { label: t.cpuCores, value: s.cpu.cores === null ? null : String(s.cpu.cores), placeholder: t.unknownValue, mono: true },
                  {
                    label: t.cpuLoadLong,
                    value: s.cpu.load ? `${f.formatLoad(s.cpu.load.five)} / ${f.formatLoad(s.cpu.load.fifteen)}` : null,
                    mono: true,
                  },
                ]}
              />
              {#if s.cpu.frequency.current_mhz === null}<p class="secondary">{t.cpuNoFreq}</p>{/if}
            </div>
          </details>
        </Card>

        <Card id="memory" title={t.memHeading} icon="memory" {stale}>
          {#if s.memory.total_bytes === null || s.memory.used_bytes === null}
            <p class="metric missing" data-testid="memory-used">{t.unavailableValue}</p>
            <Meter value={null} label={t.memUsed} />
          {:else}
            <p class="metric metric-text num" data-testid="memory-used">
              {fmt('memUsedOf', { used: f.formatBytes(s.memory.used_bytes) ?? '', total: f.formatBytes(s.memory.total_bytes) ?? '' })}
            </p>
            <Meter value={memoryUsedPercent(s)} label={t.memUsed} />
          {/if}
          <Sparkline
            values={memSeries}
            min={0}
            max={100}
            label={fmt('sparkMemory', { value: or(f.formatPercent(lastOf(memSeries)), t.unavailableValue) })}
          />
          <KeyValueList
            items={[
              { label: t.memAvailable, value: f.formatBytes(s.memory.available_bytes), mono: true },
              {
                label: t.memSwap,
                mono: true,
                testid: 'swap',
                value:
                  s.memory.swap_total_bytes === null || s.memory.swap_used_bytes === null || s.memory.swap_total_bytes === 0
                    ? t.memSwapNone
                    : fmt('memUsedOf', { used: f.formatBytes(s.memory.swap_used_bytes) ?? '', total: f.formatBytes(s.memory.swap_total_bytes) ?? '' }),
              },
            ]}
          />
        </Card>

        <Card id="temperature" title={t.tempHeading} icon="thermometer" {stale}>
          {#if s.temperatures.length === 0}
            <EmptyState compact icon="info" title={t.tempNone} titleTestid="no-sensors" />
          {:else}
            <ul class="rows">
              {#each s.temperatures as sensor (sensor.id)}
                {@const name = sensor.cpu ? t.tempSoc : sensor.name}
                <li class="line">
                  <span class="truncate" title={name}>{name}</span>
                  {#if sensor.celsius === null}
                    <strong class="secondary" data-testid="temperature-{sensor.id}">{t.unavailableValue}</strong>
                  {:else}
                    <strong class="num" data-testid="temperature-{sensor.id}">{f.formatCelsius(sensor.celsius)}</strong>
                  {/if}
                </li>
              {/each}
            </ul>
            <Sparkline
              values={tempSeries}
              min={tempRange.min}
              max={tempRange.max}
              label={fmt('sparkTemperature', { value: or(f.formatCelsius(lastOf(tempSeries)), t.unavailableValue) })}
            />
            <details>
              <summary>{t.technicalDetails}</summary>
              <div class="details-body">
                <KeyValueList
                  items={s.temperatures.map((sensor): KeyValueItem => ({
                    label: sensor.name,
                    value: `${sensor.id}, ${msg(has('tempSource.' + sensor.source) ? 'tempSource.' + sensor.source : 'tempSource.hwmon')}`,
                  }))}
                />
              </div>
            </details>
          {/if}
        </Card>

        <Card id="storage" title={t.storageHeading} icon="drive" {stale}>
          {#if s.storage.length === 0}
            <EmptyState compact icon="info" title={t.storageNone} titleTestid="no-storage" />
          {:else}
            <ul class="rows">
              {#each s.storage as disk (disk.mount_point)}
                <li class="stack-sm" data-testid="storage-{disk.mount_point}">
                  <div class="line">
                    <strong class="num truncate" title={disk.mount_point}>{disk.mount_point}</strong>
                    <span class="num">{f.formatPercent(disk.used_percent)}</span>
                  </div>
                  <Meter value={disk.used_percent} level={barLevel(disk.used_percent)} label="{t.storageHeading} {disk.mount_point}" />
                  <span class="secondary caption">
                    {fmt('storageUsedOf', { used: f.formatBytes(disk.used_bytes) ?? '', total: f.formatBytes(disk.total_bytes) ?? '' })}
                  </span>
                </li>
              {/each}
            </ul>
            <details>
              <summary>{t.technicalDetails}</summary>
              <div class="details-body">
                {#each s.storage as disk (disk.mount_point)}
                  <KeyValueList
                    items={[
                      { label: t.storageFsType, value: disk.fs_type, title: disk.mount_point },
                      { label: t.storageFree, value: f.formatBytes(disk.available_bytes), mono: true },
                    ]}
                  />
                {/each}
              </div>
            </details>
          {/if}
        </Card>

        <Card id="network" title={t.netHeading} icon="network" {stale}>
          {#if s.network.length === 0}
            <EmptyState compact icon="info" title={t.netNone} titleTestid="no-network" />
          {:else}
            <ul class="rows">
              {#each s.network as nic (nic.name)}
                {@const rate = rates[nic.name]}
                <!-- "Measuring" is only promised while live. Otherwise the last rate (dimmed with the card) or "not available". -->
                {@const rateText = (bytes: number | null | undefined) => f.formatRate(bytes ?? null) ?? (live ? t.netRateWaiting : t.unavailableValue)}
                <li class="stack-sm" data-testid="network-{nic.name}">
                  <div class="line">
                    <span class="nic">
                      <Icon name={kindIcon(nic.kind)} />
                      <strong class="num truncate" title={nic.name}>{nic.name}</strong>
                    </span>
                    <Badge tone={linkTone(nic.state)}>{linkLabel(nic.state)}</Badge>
                  </div>
                  <p class="addresses">
                    {#each [...nic.ipv4, ...nic.ipv6] as address (address)}
                      <span class="address num">{address}</span>
                    {:else}
                      <span class="secondary">{t.netNoAddress}</span>
                    {/each}
                  </p>
                  <KeyValueList
                    items={[
                      { label: t.netRx, value: rateText(rate?.rx), mono: true, testid: 'rx-' + nic.name },
                      { label: t.netTx, value: rateText(rate?.tx), mono: true, testid: 'tx-' + nic.name },
                    ]}
                  />
                  <details>
                    <summary>{t.technicalDetails}</summary>
                    <div class="details-body">
                      <KeyValueList
                        items={[
                          { label: t.netKind, value: kindLabel(nic.kind) },
                          { label: t.netSpeed, value: f.formatSpeed(nic.speed_mbps), mono: true },
                          {
                            label: t.netTotals,
                            value: `${t.netRx} ${or(f.formatBytes(nic.rx_bytes), t.unavailableValue)}, ${t.netTx} ${or(f.formatBytes(nic.tx_bytes), t.unavailableValue)}`,
                            mono: true,
                          },
                        ]}
                      />
                    </div>
                  </details>
                </li>
              {/each}
            </ul>
          {/if}
        </Card>
      </div>
    </div>
  </div>
{/if}

<style>
  .metric {
    font-size: var(--text-2xl);
    font-weight: var(--weight-semibold);
    line-height: var(--leading-tight);
    overflow-wrap: anywhere;
  }

  .metric-text {
    font-size: var(--text-lg);
    padding-block: 0.3rem;
  }

  .missing {
    font-size: var(--text-lg);
    font-weight: var(--weight-regular);
    font-family: var(--font-sans);
    color: var(--color-text-secondary);
  }

  .secondary {
    color: var(--color-text-secondary);
  }

  .caption {
    font-size: var(--text-sm);
  }

  .line {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: var(--space-3);
    min-width: 0;
  }

  .stack-sm {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: var(--space-2);
    min-width: 0;
  }

  .details-body {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: var(--space-3);
    padding: var(--space-2) 0 var(--space-1);
  }

  .nic {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    min-width: 0;
  }

  .nic :global(.icon) {
    color: var(--color-text-secondary);
  }

  .addresses {
    display: flex;
    flex-wrap: wrap;
    gap: 0 var(--space-4);
  }

  .address {
    font-size: var(--text-sm);
    overflow-wrap: anywhere;
  }
</style>
