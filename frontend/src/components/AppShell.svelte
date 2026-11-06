<script lang="ts">
  import type { Snippet } from 'svelte';
  import { t } from '../lib/i18n';
  import type { NavGroup, NavItem } from '../lib/ui';
  import BrandMark from './BrandMark.svelte';
  import Icon from './Icon.svelte';
  import LanguageSwitch from './LanguageSwitch.svelte';
  import VersionLine from './VersionLine.svelte';

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

  // Specification 7.2: the entries without a group at the top, then Workload, then System.
  // A group with no entry is not shown at all.
  const sections = $derived(
    [
      { group: undefined, label: null, entries: items.filter((i) => !i.group) },
      { group: 'workload' as NavGroup, label: t.navGroupWorkload, entries: items.filter((i) => i.group === 'workload') },
      { group: 'system' as NavGroup, label: t.navGroupSystem, entries: items.filter((i) => i.group === 'system') },
    ].filter((s) => s.entries.length > 0),
  );

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
        {#each sections as section (section.group ?? 'top')}
          {#if section.label}<p class="group" id={`nav-group-${section.group}`}>{section.label}</p>{/if}
          <ul aria-labelledby={section.label ? `nav-group-${section.group}` : undefined}>
            {#each section.entries as item (item.id)}
              <li>
                <a href={item.href} aria-current={item.id === current ? 'page' : undefined}>
                  <Icon name={item.icon} />
                  <span>{item.label}</span>
                </a>
              </li>
            {/each}
          </ul>
        {/each}
      </nav>
      {#if account}{@render account()}{/if}
      <div class="foot">
        <LanguageSwitch />
        <VersionLine />
      </div>
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
    display: flex;
    flex-direction: column;
    height: 100vh;
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

  .group {
    margin: var(--space-4) 0 var(--space-1);
    padding: 0 var(--space-3);
    font-size: var(--text-xs);
    font-weight: var(--weight-semibold);
    letter-spacing: 0.04em;
    text-transform: uppercase;
    color: var(--color-text-secondary);
  }

  .foot {
    display: grid;
    gap: var(--space-2);
    margin-top: auto;
    padding: var(--space-4) var(--space-3) 0;
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

  /* Next to the sidebar, at most --content-max wide. On a wide window the free space is at the right edge,
     not between the navigation and the content. */
  .content {
    max-width: var(--content-max);
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
      height: auto;
      overflow: visible;
      flex-direction: row;
      flex-wrap: wrap;
      align-items: center;
      gap: var(--space-1) var(--space-4);
      padding: var(--space-2) var(--space-4);
    }

    .brand {
      margin-bottom: 0;
      padding: 0;
    }

    nav {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      gap: var(--space-1) var(--space-3);
    }

    nav ul {
      display: flex;
      flex-wrap: wrap;
    }

    .group {
      margin: 0;
      padding: 0;
    }

    .foot {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      margin-top: 0;
      margin-inline-start: auto;
      padding: 0;
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
