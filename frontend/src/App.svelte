<script lang="ts">
  import { onMount } from 'svelte';
  import Device from './Device.svelte';
  import Home from './Home.svelte';
  import { t } from './lib/i18n';

  type View = 'home' | 'device';
  const viewFromHash = (): View => (location.hash === '#/device' ? 'device' : 'home');

  let view = $state<View>(viewFromHash());

  onMount(() => {
    const onHash = () => (view = viewFromHash());
    window.addEventListener('hashchange', onHash);
    return () => window.removeEventListener('hashchange', onHash);
  });
</script>

<main class:wide={view === 'home'}>
  <nav aria-label={t.navLabel}>
    <a href="#/" aria-current={view === 'home' ? 'page' : undefined}>{t.navHome}</a>
    <a href="#/device" aria-current={view === 'device' ? 'page' : undefined}>{t.navDevice}</a>
  </nav>

  {#if view === 'device'}
    <Device />
  {:else}
    <Home />
  {/if}
</main>
