<script lang="ts">
  import { onMount } from 'svelte';
  import { fetchDevice, formatCelsius, formatMemory, type Device } from './lib/device';
  import { msg, t } from './lib/i18n';

  let phase = $state<'loading' | 'error' | 'ready'>('loading');
  let device = $state<Device | null>(null);

  async function load() {
    phase = 'loading';
    const result = await fetchDevice();
    if (result.ok) {
      device = result.device;
      phase = 'ready';
    } else {
      phase = 'error';
    }
  }

  onMount(load);

  const or = (value: string | null, fallback: string = t.unknownValue) => value ?? fallback;
  const osName = (d: Device) =>
    d.os.pretty_name ?? (d.os.id ? `${d.os.id}${d.os.version_id ? ' ' + d.os.version_id : ''}` : null);
</script>

<h1>{t.deviceTitle}</h1>

{#if phase === 'loading'}
  <p role="status">{t.deviceLoading}</p>
{:else if phase === 'error' || device === null}
  <p class="status" data-state="unreachable" role="alert">{t.deviceError}</p>
  <button type="button" onclick={load}>{t.deviceReload}</button>
{:else}
  {@const s = device.support}
  <section aria-labelledby="support-heading">
    <h2 id="support-heading">{t.supportHeading}</h2>
    <p class="status" data-tier={s.tier} data-testid="tier">{msg('tier.' + s.tier)}</p>
    <p class="notice" data-tier={s.tier}>
      {#if s.tier === 'generic'}<strong>{t.genericNoticeTitle}.</strong>{/if}
      {#if s.tier === 'unsupported'}<strong>{t.unsupportedNoticeTitle}.</strong>{/if}
      {msg(s.explanation_key)}
    </p>
    <dl>
      <dt>{t.fieldProfile}</dt>
      <dd>{device.profile.id} {device.profile.version}</dd>
      <dt>{t.fieldMatchedBy}</dt>
      <dd>{msg('matchedBy.' + device.profile.matched_by)}</dd>
    </dl>
    {#if s.limitations.length > 0}
      <h3>{t.limitationsHeading}</h3>
      <ul>
        {#each s.limitations as key (key)}
          <li>{msg('limitation.' + key)}</li>
        {/each}
      </ul>
    {/if}
  </section>

  <section aria-labelledby="hardware-heading">
    <h2 id="hardware-heading">{t.hardwareHeading}</h2>
    <dl>
      <dt>{t.fieldModel}</dt>
      <dd>{or(device.hardware.model)}</dd>
      <dt>{t.fieldVendor}</dt>
      <dd>{or(device.hardware.vendor)}</dd>
      <dt>{t.fieldSoc}</dt>
      <dd>{or(device.hardware.soc_family)}</dd>
      <dt>{t.fieldArch}</dt>
      <dd>{device.hardware.arch}</dd>
      {#if device.hardware.revision_code}
        <dt>{t.fieldRevision}</dt>
        <dd>{device.hardware.revision_code}</dd>
      {/if}
      <dt>{t.fieldMemory}</dt>
      <dd>{or(formatMemory(device.hardware.memory_total_bytes))}</dd>
    </dl>
  </section>

  <section aria-labelledby="os-heading">
    <h2 id="os-heading">{t.osHeading}</h2>
    <dl>
      <dt>{t.fieldOs}</dt>
      <dd>{or(osName(device))}</dd>
      <dt>{t.fieldOsAdapter}</dt>
      <dd>
        {msg('adapter.' + device.os.adapter)}
        <span class="muted">({msg('adapterStatus.' + device.os.adapter_status)})</span>
      </dd>
      <dt>{t.fieldKernel}</dt>
      <dd>{or(device.kernel.release)}</dd>
    </dl>
  </section>

  <section aria-labelledby="sensors-heading">
    <h2 id="sensors-heading">{t.sensorsHeading}</h2>
    <dl>
      <dt>{t.fieldCpuTemp}</dt>
      <dd data-testid="cpu-temperature">
        {or(formatCelsius(device.sensors.cpu_temperature_celsius), t.unavailableValue)}
      </dd>
      <dt>{t.fieldBootMedium}</dt>
      <dd>{msg('bootMedium.' + device.boot_medium.type)}</dd>
    </dl>
  </section>

  <section aria-labelledby="capabilities-heading">
    <h2 id="capabilities-heading">{t.capabilitiesHeading}</h2>
    <p class="muted">{t.capabilitiesIntro}</p>
    <ul class="capabilities">
      {#each device.capabilities as cap (cap.id)}
        <li data-available={cap.available}>
          <span>{msg('capability.' + cap.id)}</span>
          <strong>{cap.available ? t.capAvailable : t.capUnavailable}</strong>
          <details>
            <summary>{t.capDetails}</summary>
            <p>{t.capSource}: <code>{cap.source}</code></p>
            {#if cap.reason}<p>{t.capReason}: {msg('capabilityReason.' + cap.reason)}</p>{/if}
          </details>
        </li>
      {/each}
    </ul>
  </section>

  <button type="button" onclick={load}>{t.deviceReload}</button>
{/if}
