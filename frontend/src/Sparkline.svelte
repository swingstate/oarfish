<script lang="ts">
  import { t } from './lib/i18n';

  interface Props {
    values: (number | null)[];
    min: number;
    max: number;
    /** Accessible description, for example "CPU usage over the last minutes, now 12 %". */
    label: string;
  }
  let { values, min, max, label }: Props = $props();

  const width = 120;
  const height = 32;
  const pad = 2;

  // Consecutive known values form one stroke; an unknown value breaks it.
  const path = $derived.by(() => {
    const n = values.length;
    const span = max - min || 1;
    let d = '';
    let open = false;
    values.forEach((v, i) => {
      if (v === null || !Number.isFinite(v)) {
        open = false;
        return;
      }
      const x = n === 1 ? width / 2 : (i / (n - 1)) * width;
      const y = height - pad - Math.min(1, Math.max(0, (v - min) / span)) * (height - 2 * pad);
      d += `${open ? 'L' : 'M'}${x.toFixed(1)} ${y.toFixed(1)}`;
      open = true;
      // A lone point still gets a dot (round line caps draw a zero-length line).
      if (i === n - 1 || values[i + 1] === null) d += ` l0 0`;
    });
    return d;
  });
  const known = $derived(values.filter((v) => v !== null).length);
</script>

{#if known >= 2}
  <svg class="spark" viewBox="0 0 {width} {height}" preserveAspectRatio="none" role="img" aria-label={label}>
    <path d={path} />
  </svg>
{:else}
  <p class="muted small spark-empty">{t.sparkNoData}</p>
{/if}
