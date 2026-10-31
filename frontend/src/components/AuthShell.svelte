<script lang="ts">
  import type { Snippet } from 'svelte';
  import { t } from '../lib/i18n';
  import BrandMark from './BrandMark.svelte';

  interface Props {
    children: Snippet;
  }
  let { children }: Props = $props();
</script>

<!-- The frame of the pages shown before sign-in (loading, setup, login): the
     brand and one narrow column, no navigation and no device data. -->
<div class="auth">
  <header>
    <span class="brand"><BrandMark /><span>{t.brandName}</span></span>
  </header>
  <main id="main" tabindex="-1">
    <div class="panel">{@render children()}</div>
  </main>
</div>

<style>
  .auth {
    min-height: 100vh;
    display: grid;
    grid-template-rows: auto 1fr;
  }

  header {
    padding: var(--space-3) var(--space-4);
    background: var(--color-surface);
    border-block-end: 1px solid var(--color-border);
  }

  .brand {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    min-height: 2.5rem;
    font-size: var(--text-lg);
    font-weight: var(--weight-semibold);
  }

  main {
    padding: var(--space-6) var(--space-4) var(--space-7);
  }

  main:focus {
    outline: none;
  }

  .panel {
    max-width: 30rem;
    margin-inline: auto;
  }

  @media (forced-colors: active) {
    header {
      border-color: CanvasText;
    }
  }
</style>
