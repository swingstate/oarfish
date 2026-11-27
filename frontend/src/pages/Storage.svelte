<script lang="ts">
  import { onMount } from 'svelte';
  import Card from '../components/Card.svelte';
  import ConnectionIndicator from '../components/ConnectionIndicator.svelte';
  import EmptyState from '../components/EmptyState.svelte';
  import KeyValueList from '../components/KeyValueList.svelte';
  import Meter from '../components/Meter.svelte';
  import Notice from '../components/Notice.svelte';
  import PageHeader from '../components/PageHeader.svelte';
  import { fetchDevice, type Device } from '../lib/device';
  import * as f from '../lib/format';
  import { fmt, has, msg, t } from '../lib/i18n';
  import { LiveSnapshot } from '../lib/live.svelte';
  import type { OtherMount, Storage } from '../lib/metrics';
  import { capacityLevel, capacityWarning, deviceText, mountKindNote, mountKindText, rootDisk, storageMarks } from '../lib/storage';
  import type { KeyValueItem, MeterLevel } from '../lib/ui';

  // Task ST1 (ORB-STO-001 to -003, the low-capacity part of -004, -005, -006;
  // docs/UI.md, "Storage page"). Read-only: no mount, no format, no health
  // value, no write-load hint. The live data path of the Dashboard; the boot
  // medium comes from the detection (GET /api/v1/device), read once.
  const live = new LiveSnapshot();
  let device = $state<Device | null>(null);
  onMount(() => {
    void fetchDevice().then((r) => {
      if (r.ok) device = r.device;
    });
    return live.start();
  });

  const s = $derived(live.snapshot);
  const lastUpdate = $derived(s ? f.formatClock(s.collected_at) : null);
  const root = $derived(s ? rootDisk(s.storage) : '');
  const rootDevice = $derived(s?.storage.find((d) => d.mount_point === '/')?.device ?? '');

  const meterLevel = (p: number): MeterLevel => {
    const l = capacityLevel(p);
    return l === 'critical' ? 'critical' : l === 'relevant' ? 'relevant' : 'ok';
  };

  function fsFacts(d: Storage): KeyValueItem[] {
    return [
      { label: t.stoDevice, value: deviceText(d), mono: true, testid: 'device-' + d.mount_point },
      { label: t.storageFsType, value: d.fs_type, mono: true },
      { label: t.stoTotal, value: f.formatBytes(d.total_bytes), mono: true },
      { label: t.stoUsed, value: fmt('stoUsedValue', { used: f.formatBytes(d.used_bytes) ?? '', percent: f.formatPercent(d.used_percent) ?? '' }), mono: true },
      { label: t.storageFree, value: f.formatBytes(d.available_bytes), mono: true },
    ];
  }

  function otherFacts(m: OtherMount): KeyValueItem[] {
    const used =
      m.used_bytes === null || m.used_percent === null
        ? null
        : fmt('stoUsedValue', { used: f.formatBytes(m.used_bytes) ?? '', percent: f.formatPercent(m.used_percent) ?? '' });
    return [
      { label: t.stoDevice, value: mountKindText(m.kind), testid: 'kind-' + m.mount_point },
      { label: t.storageFsType, value: m.fs_type, mono: true },
      { label: t.stoTotal, value: f.formatBytes(m.total_bytes), mono: true },
      { label: t.stoUsed, value: used, mono: true },
      { label: t.storageFree, value: f.formatBytes(m.available_bytes), mono: true },
    ];
  }

  const mediumText = (d: Device | null) => (d ? msg(has('bootMedium.' + d.boot_medium.type) ? 'bootMedium.' + d.boot_medium.type : 'bootMedium.unknown') : null);
</script>

<PageHeader title={t.storagePageTitle}>
  {#snippet aside()}<ConnectionIndicator state={live.conn} />{/snippet}
</PageHeader>

<div data-testid="storage-page">
  {#if s === null}
    {#if live.loadFailed && (live.conn === 'reconnecting' || live.conn === 'stale')}
      <EmptyState icon="warning" tone="warning" title={t.stoPageLoadError} description={t.stoPageLoadErrorHint} role="alert" data-state="load-error">
        <button type="button" onclick={() => void live.load()}>{t.dashboardRetry}</button>
      </EmptyState>
    {:else}
      <EmptyState icon="clock" title={t.stoPageLoading} role="status" data-state="loading" />
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
        <Card id="boot" title={t.stoBootHeading} icon="drive" stale={live.stale} data-testid="boot">
          <KeyValueList
            items={[
              { label: t.stoBootKind, value: mediumText(device), testid: 'boot-kind' },
              { label: t.stoRootDevice, value: rootDevice === '' ? null : rootDevice, mono: true, testid: 'root-device' },
            ]}
          />
        </Card>

        <Card id="filesystems" title={t.stoFsHeading} icon="drive" stale={live.stale} wide data-testid="filesystems">
          {#if s.storage.length === 0}
            <EmptyState compact icon="info" title={t.stoNone} titleTestid="no-filesystems" />
          {:else}
            <ul class="rows">
              {#each s.storage as d (d.mount_point)}
                {@const warning = capacityWarning(d)}
                <li class="stack-sm" data-testid="fs-{d.mount_point}">
                  <h3 class="mount num truncate" title={d.mount_point}>{d.mount_point}</h3>
                  <Meter value={d.used_percent} level={meterLevel(d.used_percent)} label="{t.stoUsed} {d.mount_point}" />
                  {#each storageMarks(d, root) as mark (mark)}
                    <p class="mark" data-testid="mark-{d.mount_point}">{mark}</p>
                  {/each}
                  {#if warning}
                    <Notice tone={warning.tone} data-testid="warning-{d.mount_point}"><p>{warning.text}</p></Notice>
                  {/if}
                  <KeyValueList items={fsFacts(d)} />
                </li>
              {/each}
            </ul>
          {/if}
        </Card>

        {#if s.mounts && s.mounts.length > 0}
          <Card id="mounts" title={t.stoOtherHeading} icon="drive" stale={live.stale} wide data-testid="mounts">
            <p class="secondary">{t.stoOtherIntro}</p>
            <ul class="rows">
              {#each s.mounts as m (m.mount_point)}
                <li class="stack-sm" data-testid="mount-{m.mount_point}">
                  <h3 class="mount num truncate" title={m.mount_point}>{m.mount_point}</h3>
                  <p class="mark" data-testid="kind-note-{m.mount_point}">{mountKindNote(m.kind)}</p>
                  <KeyValueList items={otherFacts(m)} />
                </li>
              {/each}
            </ul>
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
  .mount {
    margin: 0;
    font-size: var(--text-md);
    font-weight: var(--weight-semibold);
  }
  .mark {
    margin: 0;
    font-size: var(--text-sm);
  }
  .secondary {
    margin: 0;
    color: var(--color-text-secondary);
  }
</style>
