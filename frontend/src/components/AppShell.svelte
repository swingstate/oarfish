<script lang="ts">
  import type { Snippet } from 'svelte';
  import { t } from '../lib/i18n';
  import type { NavItem } from '../lib/ui';
  import BrandMark from './BrandMark.svelte';
  import Icon from './Icon.svelte';

  interface Props {
    /** Navigation entries, in order. A new page adds one entry to the list, nothing here. */
    items: NavItem[];
    /** The id of the entry whose page is shown. */
    current: string;
    /** The user menu (account and logout), after the navigation. */
    account?: Snippet;
    children: Snippet;
  }
  let { items, current, account, children }: Props = $props();

  let main: HTMLElement | undefined = $state();

  // The skip link must not change the address: the address is the router.
  function skip(event: MouseEvent): void {
    event.preventDefault();
    main?.focus();
  }
</script>

<a class="skip-link" href="#main" onclick={skip}>{t.skipLink}</a>

<div class="shell">
  <header>
    <div class="side">
      <a class="brand" href={items[0]?.href ?? '#/'}>
        <BrandMark />
        <span>{t.brandName}</span>
      </a>
      <nav aria-label={t.navLabel}>
        <ul>
          {#each items as item (item.id)}
            <li>
              <a href={item.href} aria-current={item.id === current ? 'page' : undefined}>
                <Icon name={item.icon} />
                <span>{item.label}</span>
              </a>
            </li>
          {/each}
        </ul>
      </nav>
      {#if account}{@render account()}{/if}
    </div>
  </header>

  <main id="main" tabindex="-1" bind:this={main}>
    <div class="content">{@render children()}</div>
  </main>
</div>

<style>
  .skip-link {
    position: absolute;
    inset-block-start: var(--space-2);
    inset-inline-start: var(--space-2);
    z-index: 10;
    padding: var(--space-2) var(--space-4);
    font-weight: var(--weight-medium);
    color: var(--color-accent-fg);
    background: var(--color-accent);
    border-radius: var(--radius-md);
    /* Out of sight until it has the focus. */
    transform: translateY(-300%);
  }

  .skip-link:focus {
    transform: none;
  }

  .shell {
    display: grid;
    grid-template-columns: var(--sidebar-width) minmax(0, 1fr);
    min-height: 100vh;
  }

  /* The header column is as tall as the page, so its background never ends early. */
  header {
    background: var(--color-surface);
    border-inline-end: 1px solid var(--color-border);
  }

  .side {
    position: sticky;
    inset-block-start: 0;
    max-height: 100vh;
    overflow-y: auto;
    padding: var(--space-4) var(--space-3);
  }

  .brand {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    min-height: 2.5rem;
    margin-bottom: var(--space-4);
    padding: 0 var(--space-3);
    font-size: var(--text-lg);
    font-weight: var(--weight-semibold);
    color: var(--color-text);
    text-decoration: none;
  }

  nav ul {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: var(--space-1);
  }

  nav a {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    min-height: 2.5rem;
    padding: var(--space-1) var(--space-3);
    font-weight: var(--weight-medium);
    color: var(--color-text-secondary);
    text-decoration: none;
    border-radius: var(--radius-md);
    transition: background-color var(--duration-fast) var(--ease-standard);
  }

  nav a:hover {
    color: var(--color-text);
    background: var(--color-surface-muted);
  }

  nav a[aria-current='page'] {
    color: var(--color-accent-subtle-fg);
    background: var(--color-accent-subtle-bg);
    font-weight: var(--weight-semibold);
    box-shadow: inset 3px 0 0 var(--color-accent);
  }

  main {
    min-width: 0;
    padding: var(--space-5) var(--space-5) var(--space-7);
  }

  main:focus {
    outline: none;
  }

  .content {
    max-width: var(--content-max);
    margin-inline: auto;
  }

  /* Below 60rem the sidebar becomes a top bar: the brand, then the entries in a row that wraps. */
  @media (max-width: 59.999rem) {
    .shell {
      display: block;
    }

    header {
      border-inline-end: 0;
      border-block-end: 1px solid var(--color-border);
    }

    .side {
      position: static;
      max-height: none;
      overflow: visible;
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      gap: var(--space-1) var(--space-4);
      padding: var(--space-2) var(--space-4);
    }

    .brand {
      margin-bottom: 0;
      padding: 0;
    }

    nav ul {
      display: flex;
      flex-wrap: wrap;
    }

    nav a[aria-current='page'] {
      box-shadow: inset 0 -3px 0 var(--color-accent);
    }

    main {
      padding: var(--space-4) var(--space-4) var(--space-6);
    }
  }

  @media (forced-colors: active) {
    header {
      border-color: CanvasText;
    }

    nav a[aria-current='page'] {
      border: 1px solid Highlight;
    }
  }
</style>
