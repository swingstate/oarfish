<script lang="ts">
  import { onMount } from 'svelte';
  import AlertList from './AlertList.svelte';
  import Sparkline from './Sparkline.svelte';
  import * as f from './lib/format';
  import { fmt, msg, t } from './lib/i18n';
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
  } from './lib/metrics';
  import { MetricsStream, type StreamState } from './lib/stream';

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
    });
    stream.start();
    void load();
    return () => stream.stop();
  });

  const or = (value: string | null | undefined, fallback: string = t.unknownValue) => value ?? fallback;
  const lastUpdate = $derived(snapshot ? f.formatClock(snapshot.collected_at) : null);

  /** Range of the temperature sparkline: a few degrees around what was seen. */
  const tempRange = $derived.by(() => {
    const known = tempSeries.filter((v): v is number => v !== null);
    if (known.length === 0) return { min: 20, max: 80 };
    const lo = Math.floor(Math.min(...known)) - 2;
    return { min: lo, max: Math.max(Math.ceil(Math.max(...known)) + 2, lo + 10) };
  });

  const lastOf = (series: (number | null)[]) => series.findLast((v) => v !== null) ?? null;
  const barLevel = (percent: number) => (percent > 95 ? 'critical' : percent > 85 ? 'relevant' : 'ok');
</script>

{#snippet staleBadge()}
  {#if stale}<span class="badge">{t.staleBadge}</span>{/if}
{/snippet}

<h1>{t.homeTitle}</h1>

{#if snapshot === null}
  {#if loadFailed && (conn === 'reconnecting' || conn === 'stale')}
    <p class="status" data-state="unreachable">{t.homeLoadError}</p>
    <button type="button" onclick={load}>{t.homeRetry}</button>
  {:else}
    <p data-testid="home-loading">{conn === 'connecting' ? t.homeLoading : t.connConnecting}</p>
  {/if}
{:else}
  {@const s = snapshot}
  {#if stale}
    <p class="notice conn" data-state={conn} data-testid="connection-notice">
      {conn === 'reconnecting' ? t.connLost : t.connStale}
      {#if lastUpdate}<span class="muted">{fmt('connLastUpdate', { time: lastUpdate })}</span>{/if}
    </p>
  {/if}

  <div class="home" data-stale={stale} data-testid="home">
    <section class="wide-card" aria-labelledby="alerts-heading">
      <h2 id="alerts-heading">{t.alertsHeading}</h2>
      <AlertList {alerts} />
    </section>

    <section class="wide-card" aria-labelledby="summary-heading">
      <h2 id="summary-heading">{t.summaryHeading} {@render staleBadge()}</h2>
      <dl class="summary">
        <dt>{t.sumHost}</dt>
        <dd data-testid="hostname">{or(s.hostname)}</dd>
        <dt>{t.sumBoard}</dt>
        <dd>
          {or(device?.model)}
          {#if device?.soc_family}<span class="muted">({device.soc_family})</span>{/if}
        </dd>
        <dt>{t.sumOs}</dt>
        <dd>{or(device?.os_pretty_name)}</dd>
        <dt>{t.sumUptime}</dt>
        <dd data-testid="uptime">{or(f.formatUptime(s.uptime_seconds))}</dd>
        <dt>{t.sumSupport}</dt>
        <dd>
          {#if device?.support_tier}
            <span class="tier" data-tier={device.support_tier} data-testid="tier">{msg('tier.' + device.support_tier)}</span>
          {:else}
            {t.unknownValue}
          {/if}
          <a href="#/device">{t.sumDeviceLink}</a>
        </dd>
      </dl>
    </section>

    <div class="cards">
      <section class="card" aria-labelledby="cpu-heading">
        <h2 id="cpu-heading">{t.cpuHeading} {@render staleBadge()}</h2>
        <p class="big" data-testid="cpu-usage">{or(f.formatPercent(s.cpu.usage_percent), t.unavailableValue)}</p>
        <progress class="bar" max="100" value={s.cpu.usage_percent ?? 0} aria-label={t.cpuUsage}></progress>
        <Sparkline
          values={cpuSeries}
          min={0}
          max={100}
          label={fmt('sparkCpu', { value: or(f.formatPercent(lastOf(cpuSeries)), t.unavailableValue) })}
        />
        <dl>
          <dt>{t.cpuLoad}</dt>
          <dd>{s.cpu.load ? f.formatLoad(s.cpu.load.one) : t.unavailableValue}</dd>
          <dt>{t.cpuFreq}</dt>
          <dd data-testid="cpu-frequency">
            {#if s.cpu.frequency.current_mhz === null}
              {t.unavailableValue}
            {:else if s.cpu.frequency.max_mhz === null}
              {f.formatMHz(s.cpu.frequency.current_mhz)}
            {:else}
              {fmt('cpuFreqOfMax', { current: f.formatMHz(s.cpu.frequency.current_mhz) ?? '', max: f.formatMHz(s.cpu.frequency.max_mhz) ?? '' })}
            {/if}
          </dd>
        </dl>
        <details>
          <summary>{t.technicalDetails}</summary>
          <dl>
            <dt>{t.cpuGovernor}</dt>
            <dd>{or(s.cpu.governor, t.unavailableValue)}</dd>
            <dt>{t.cpuCores}</dt>
            <dd>{s.cpu.cores ?? t.unknownValue}</dd>
            <dt>{t.cpuLoadLong}</dt>
            <dd>{s.cpu.load ? `${f.formatLoad(s.cpu.load.five)} / ${f.formatLoad(s.cpu.load.fifteen)}` : t.unavailableValue}</dd>
          </dl>
          {#if s.cpu.frequency.current_mhz === null}<p class="muted">{t.cpuNoFreq}</p>{/if}
        </details>
      </section>

      <section class="card" aria-labelledby="memory-heading">
        <h2 id="memory-heading">{t.memHeading} {@render staleBadge()}</h2>
        {#if s.memory.total_bytes === null || s.memory.used_bytes === null}
          <p class="big" data-testid="memory-used">{t.unavailableValue}</p>
        {:else}
          {@const pct = memoryUsedPercent(s) ?? 0}
          <p class="big" data-testid="memory-used">
            {fmt('memUsedOf', { used: f.formatBytes(s.memory.used_bytes) ?? '', total: f.formatBytes(s.memory.total_bytes) ?? '' })}
          </p>
          <progress class="bar" max="100" value={pct} aria-label={t.memUsed}></progress>
        {/if}
        <Sparkline
          values={memSeries}
          min={0}
          max={100}
          label={fmt('sparkMemory', { value: or(f.formatPercent(lastOf(memSeries)), t.unavailableValue) })}
        />
        <dl>
          <dt>{t.memAvailable}</dt>
          <dd>{or(f.formatBytes(s.memory.available_bytes), t.unavailableValue)}</dd>
          <dt>{t.memSwap}</dt>
          <dd data-testid="swap">
            {#if s.memory.swap_total_bytes === null || s.memory.swap_used_bytes === null || s.memory.swap_total_bytes === 0}
              {t.memSwapNone}
            {:else}
              {fmt('memUsedOf', { used: f.formatBytes(s.memory.swap_used_bytes) ?? '', total: f.formatBytes(s.memory.swap_total_bytes) ?? '' })}
            {/if}
          </dd>
        </dl>
      </section>

      <section class="card" aria-labelledby="temperature-heading">
        <h2 id="temperature-heading">{t.tempHeading} {@render staleBadge()}</h2>
        {#if s.temperatures.length === 0}
          <p class="muted" data-testid="no-sensors">{t.tempNone}</p>
        {:else}
          <ul class="plain sensors">
            {#each s.temperatures as sensor (sensor.id)}
              <li>
                <span>{sensor.cpu ? t.tempSoc : sensor.name}</span>
                <strong data-testid="temperature-{sensor.id}">{or(f.formatCelsius(sensor.celsius), t.unavailableValue)}</strong>
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
            <dl>
              {#each s.temperatures as sensor (sensor.id)}
                <dt>{sensor.name}</dt>
                <dd>{sensor.id}, {msg('tempSource.' + sensor.source)}</dd>
              {/each}
            </dl>
          </details>
        {/if}
      </section>

      <section class="card" aria-labelledby="storage-heading">
        <h2 id="storage-heading">{t.storageHeading} {@render staleBadge()}</h2>
        {#if s.storage.length === 0}
          <p class="muted" data-testid="no-storage">{t.storageNone}</p>
        {:else}
          <ul class="plain">
            {#each s.storage as disk (disk.mount_point)}
              <li data-testid="storage-{disk.mount_point}">
                <div class="row">
                  <strong>{disk.mount_point}</strong>
                  <span>{f.formatPercent(disk.used_percent)}</span>
                </div>
                <progress
                  class="bar"
                  data-level={barLevel(disk.used_percent)}
                  max="100"
                  value={disk.used_percent}
                  aria-label="{t.storageHeading} {disk.mount_point}"
                ></progress>
                <span class="muted">
                  {fmt('storageUsedOf', { used: f.formatBytes(disk.used_bytes) ?? '', total: f.formatBytes(disk.total_bytes) ?? '' })}
                </span>
              </li>
            {/each}
          </ul>
          <details>
            <summary>{t.technicalDetails}</summary>
            <dl>
              {#each s.storage as disk (disk.mount_point)}
                <dt>{disk.mount_point}</dt>
                <dd>{t.storageFsType}: {disk.fs_type}, {t.storageFree}: {f.formatBytes(disk.available_bytes)}</dd>
              {/each}
            </dl>
          </details>
        {/if}
      </section>

      <section class="card" aria-labelledby="network-heading">
        <h2 id="network-heading">{t.netHeading} {@render staleBadge()}</h2>
        {#if s.network.length === 0}
          <p class="muted" data-testid="no-network">{t.netNone}</p>
        {:else}
          <ul class="plain">
            {#each s.network as nic (nic.name)}
              {@const rate = rates[nic.name]}
              <li data-testid="network-{nic.name}">
                <div class="row">
                  <strong>{nic.name}</strong>
                  <span class="link" data-up={nic.link_up}>{msg('linkState.' + nic.state)}</span>
                </div>
                <p class="addresses">
                  {#each [...nic.ipv4, ...nic.ipv6] as address (address)}
                    <span class="address">{address}</span>
                  {:else}
                    <span class="muted">{t.netNoAddress}</span>
                  {/each}
                </p>
                <dl>
                  <dt>{t.netRx}</dt>
                  <dd data-testid="rx-{nic.name}">{f.formatRate(rate?.rx ?? null) ?? t.netRateWaiting}</dd>
                  <dt>{t.netTx}</dt>
                  <dd data-testid="tx-{nic.name}">{f.formatRate(rate?.tx ?? null) ?? t.netRateWaiting}</dd>
                </dl>
                <details>
                  <summary>{t.technicalDetails}</summary>
                  <dl>
                    <dt>{t.netKind}</dt>
                    <dd>{msg('netKind.' + nic.kind)}</dd>
                    <dt>{t.netSpeed}</dt>
                    <dd>{or(f.formatSpeed(nic.speed_mbps), t.unavailableValue)}</dd>
                    <dt>{t.netTotals}</dt>
                    <dd>{t.netRx} {or(f.formatBytes(nic.rx_bytes), t.unavailableValue)}, {t.netTx} {or(f.formatBytes(nic.tx_bytes), t.unavailableValue)}</dd>
                  </dl>
                </details>
              </li>
            {/each}
          </ul>
        {/if}
      </section>
    </div>
  </div>
{/if}
