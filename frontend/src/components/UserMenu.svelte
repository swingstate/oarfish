<script lang="ts">
  import { t } from '../lib/i18n';
  import Icon from './Icon.svelte';

  interface Props {
    username: string;
    /** Address of the account page. */
    accountHref: string;
    current: boolean;
    onLogout: () => void;
    busy?: boolean;
  }
  let { username, accountHref, current, onLogout, busy = false }: Props = $props();
</script>

<!-- A native disclosure: the user name opens the account link and the logout
     button. Keyboard: Tab to the summary, Enter or Space opens it. -->
<details class="user-menu">
  <summary aria-label={`${t.userMenuLabel}: ${username}`}>
    <Icon name="user" />
    <span class="name truncate" title={username}>{username}</span>
  </summary>
  <ul>
    <li><a href={accountHref} aria-current={current ? 'page' : undefined}>{t.navAccount}</a></li>
    <li><button type="button" onclick={onLogout} disabled={busy}>{t.logout}</button></li>
  </ul>
</details>

<style>
  .user-menu {
    margin-top: var(--space-4);
    padding: var(--space-2) var(--space-3);
    border-top: 1px solid var(--color-border);
  }

  summary {
    min-height: 2.5rem;
    color: var(--color-text);
    font-size: var(--text-md);
    font-weight: var(--weight-medium);
  }

  .name {
    max-width: 10rem;
  }

  ul {
    display: grid;
    gap: var(--space-2);
    padding: var(--space-2) 0 0 var(--space-5);
  }

  a {
    display: inline-flex;
    align-items: center;
    min-height: var(--target-min);
  }

  a[aria-current='page'] {
    font-weight: var(--weight-semibold);
  }

  @media (max-width: 59.999rem) {
    .user-menu {
      margin-top: 0;
      margin-inline-start: auto;
      padding: 0;
      border-top: 0;
    }
  }
</style>
