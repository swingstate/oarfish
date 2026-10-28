<script lang="ts">
  import type { Snippet } from 'svelte';

  interface Props {
    title: string;
    /** Something small at the right: the connection indicator on Home. */
    aside?: Snippet;
  }
  let { title, aside }: Props = $props();
</script>

<div class="page-header">
  <h1>{title}</h1>
  {#if aside}<div class="aside">{@render aside()}</div>{/if}
</div>

<style>
  .page-header {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-2) var(--space-4);
    margin-bottom: var(--space-4);
  }

  h1 {
    min-width: 0;
    font-size: var(--text-xl);
    overflow-wrap: anywhere;
  }
</style>
