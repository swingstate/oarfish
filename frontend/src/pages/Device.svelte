<script lang="ts">
  import { onMount } from 'svelte';
  import Badge from '../components/Badge.svelte';
  import Card from '../components/Card.svelte';
  import EmptyState from '../components/EmptyState.svelte';
  import KeyValueList from '../components/KeyValueList.svelte';
  import Notice from '../components/Notice.svelte';
  import PageHeader from '../components/PageHeader.svelte';
  import { fetchDevice, formatCelsius, formatMemory, type Device } from '../lib/device';
  import { has, msg, t } from '../lib/i18n';
  import { tierTone, type Tone } from '../lib/ui';

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

  const osName = (d: Device) =>
    d.os.pretty_name ?? (d.os.id ? `${d.os.id}${d.os.version_id ? ' ' + d.os.version_id : ''}` : null);

  /** The explanation of the support status is a notice. Only a problem tier is coloured. */
  const noticeTone = (tier: string): Tone => (tier === 'generic' ? 'information' : tier === 'unsupported' || tier === 'experimental' ? tierTone(tier) : 'neutral');
  const label = (prefix: string, value: string) => msg(has(prefix + value) ? prefix + value : prefix + 'unknown');
</script>

<PageHeader title={t.deviceTitle} />

{#if phase === 'loading'}
  <EmptyState icon="clock" title={t.deviceLoading} role="status" />
{:else if phase === 'error' || device === null}
  <EmptyState icon="warning" tone="warning" title={t.deviceError} description={t.deviceErrorHint} role="alert" data-state="unreachable">
    <button type="button" onclick={load}>{t.deviceReload}</button>
  </EmptyState>
{:else}
  {@const s = device.support}
  <div class="card-grid">
    <Card id="support" title={t.supportHeading} wide>
      <div class="tier">
        <Badge tone={tierTone(s.tier)} data-testid="tier">{msg('tier.' + s.tier)}</Badge>
      </div>
      <Notice tone={noticeTone(s.tier)} data-tier={s.tier}>
        <p>
          {#if s.tier === 'generic'}<strong>{t.genericNoticeTitle}.</strong>{/if}
          {#if s.tier === 'unsupported'}<strong>{t.unsupportedNoticeTitle}.</strong>{/if}
          {msg(s.explanation_key)}
        </p>
      </Notice>
      <KeyValueList
        items={[
          { label: t.fieldProfile, value: `${device.profile.id} ${device.profile.version}`, mono: true },
          { label: t.fieldMatchedBy, value: msg('matchedBy.' + device.profile.matched_by) },
        ]}
      />
      {#if s.limitations.length > 0}
        <div class="limitations">
          <h3>{t.limitationsHeading}</h3>
          <ul>
            {#each s.limitations as key (key)}
              <li>{msg('limitation.' + key)}</li>
            {/each}
          </ul>
        </div>
      {/if}
    </Card>

    <Card id="hardware" title={t.hardwareHeading} icon="device">
      <KeyValueList
        items={[
          { label: t.fieldModel, value: device.hardware.model, placeholder: t.unknownValue },
          { label: t.fieldVendor, value: device.hardware.vendor, placeholder: t.unknownValue },
          { label: t.fieldSoc, value: device.hardware.soc_family, placeholder: t.unknownValue },
          { label: t.fieldArch, value: device.hardware.arch },
          ...(device.hardware.revision_code ? [{ label: t.fieldRevision, value: device.hardware.revision_code, mono: true }] : []),
          { label: t.fieldMemory, value: formatMemory(device.hardware.memory_total_bytes), placeholder: t.unknownValue, mono: true },
        ]}
      />
    </Card>

    <Card id="os" title={t.osHeading} icon="chip">
      <KeyValueList
        items={[
          { label: t.fieldOs, value: osName(device), placeholder: t.unknownValue },
          { label: t.fieldOsAdapter, value: label('adapter.', device.os.adapter), note: label('adapterStatus.', device.os.adapter_status) },
          { label: t.fieldKernel, value: device.kernel.release, placeholder: t.unknownValue, mono: true },
        ]}
      />
    </Card>

    <Card id="sensors" title={t.sensorsHeading} icon="thermometer">
      <KeyValueList
        items={[
          { label: t.fieldCpuTemp, value: formatCelsius(device.sensors.cpu_temperature_celsius), mono: true, testid: 'cpu-temperature' },
          { label: t.fieldBootMedium, value: msg('bootMedium.' + device.boot_medium.type) },
        ]}
      />
    </Card>

    <Card id="capabilities" title={t.capabilitiesHeading} icon="success" wide>
      <p class="secondary">{t.capabilitiesIntro}</p>
      <ul class="capabilities rows">
        {#each device.capabilities as cap (cap.id)}
          <li data-available={cap.available}>
            <div class="line">
              <span>{msg('capability.' + cap.id)}</span>
              {#if cap.available}
                <Badge tone="success" icon="success">{t.capAvailable}</Badge>
              {:else}
                <Badge tone="neutral">{t.capUnavailable}</Badge>
              {/if}
            </div>
            <details>
              <summary>{t.capDetails}</summary>
              <div class="details-body">
                <p>{t.capSource}: <code>{cap.source}</code></p>
                {#if cap.reason}<p>{t.capReason}: {msg('capabilityReason.' + cap.reason)}</p>{/if}
              </div>
            </details>
          </li>
        {/each}
      </ul>
    </Card>
  </div>

  <div class="actions">
    <button type="button" onclick={load}>{t.deviceReload}</button>
  </div>
{/if}

<style>
  .tier {
    display: flex;
  }

  .tier :global(.badge) {
    min-height: 1.75rem;
    padding: 0 var(--space-3);
    font-size: var(--text-sm);
  }

  .limitations {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: var(--space-2);
  }

  h3 {
    font-size: var(--text-sm);
    letter-spacing: 0.04em;
    text-transform: uppercase;
    color: var(--color-text-secondary);
  }

  .limitations ul {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: var(--space-1);
    padding-inline-start: var(--space-4);
    list-style: disc;
  }

  .secondary {
    color: var(--color-text-secondary);
  }

  .line {
    display: flex;
    flex-wrap: wrap;
    overflow-wrap: anywhere;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-1) var(--space-3);
  }

  .details-body {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: var(--space-1);
    padding: var(--space-1) 0;
    font-size: var(--text-sm);
    color: var(--color-text-secondary);
    overflow-wrap: anywhere;
  }

  .actions {
    margin-top: var(--space-4);
  }
</style>
