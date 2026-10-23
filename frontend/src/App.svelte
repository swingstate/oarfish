<script lang="ts">
  import { onMount } from 'svelte';
  import { fetchHealth, type HealthState } from './lib/health';
  import { t } from './lib/i18n';

  let health = $state<HealthState>('checking');

  async function check() {
    health = 'checking';
    health = await fetchHealth();
  }

  onMount(check);
</script>

<main>
  <h1>Oarfish</h1>
  <p class="tagline">{t.tagline}</p>

  <section aria-labelledby="status-heading">
    <h2 id="status-heading">{t.statusLabel}</h2>
    <p class="status" data-state={health} role="status">{t[health]}</p>
    <button type="button" onclick={check} disabled={health === 'checking'}>{t.recheck}</button>
  </section>

  <p class="note">{t.foundation}</p>
</main>
