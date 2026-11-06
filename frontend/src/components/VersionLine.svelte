<script lang="ts">
  import { onMount } from 'svelte';
  import { fmt } from '../lib/i18n';
  import { fetchVersion } from '../lib/version';

  let version = $state<string | null>(null);

  onMount(() => {
    let alive = true;
    void fetchVersion().then((v) => {
      if (alive) version = v;
    });
    return () => {
      alive = false;
    };
  });
</script>

{#if version}
  <p class="version" data-testid="version">{fmt('versionLine', { version })}</p>
{/if}

<style>
  .version {
    font-size: var(--text-xs);
    color: var(--color-text-secondary);
    overflow-wrap: anywhere;
  }
</style>
