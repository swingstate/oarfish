<script lang="ts">
  import { onMount } from 'svelte';
  import Device from './Device.svelte';
  import { fetchHealth, type HealthState } from './lib/health';
  import { t } from './lib/i18n';

  type View = 'status' | 'device';
  const viewFromHash = (): View => (location.hash === '#/device' ? 'device' : 'status');

  let view = $state<View>(viewFromHash());
  let health = $state<HealthState>('checking');

  async function check() {
    health = 'checking';
    health = await fetchHealth();
  }

  onMount(() => {
    const onHash = () => (view = viewFromHash());
    window.addEventListener('hashchange', onHash);
    check();
    return () => window.removeEventListener('hashchange', onHash);
  });
</script>

<main>
  <nav aria-label={t.navLabel}>
    <a href="#/" aria-current={view === 'status' ? 'page' : undefined}>{t.navStatus}</a>
    <a href="#/device" aria-current={view === 'device' ? 'page' : undefined}>{t.navDevice}</a>
  </nav>

  {#if view === 'device'}
    <Device />
  {:else}
    <h1>Oarfish</h1>
    <p class="tagline">{t.tagline}</p>

    <section aria-labelledby="status-heading">
      <h2 id="status-heading">{t.statusLabel}</h2>
      <p class="status" data-state={health} role="status">{t[health]}</p>
      <button type="button" onclick={check} disabled={health === 'checking'}>{t.recheck}</button>
    </section>

    <p class="note">{t.foundation}</p>
  {/if}
</main>
