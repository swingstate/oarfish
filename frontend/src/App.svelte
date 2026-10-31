<script lang="ts">
  import { onMount } from 'svelte';
  import AppShell from './components/AppShell.svelte';
  import AuthShell from './components/AuthShell.svelte';
  import EmptyState from './components/EmptyState.svelte';
  import UserMenu from './components/UserMenu.svelte';
  import Login from './pages/Login.svelte';
  import Setup from './pages/Setup.svelte';
  import { t } from './lib/i18n';
  import { fetchSession, logout, onSessionLost, sessionUser, type SessionInfo } from './lib/session';
  import type { NavItem } from './lib/ui';
  import { routeFromHash, routes } from './routes';

  // Nothing of the app is shown before the session state is known (ADR 0009):
  // first "checking", then setup, login or the app.
  type Phase = 'checking' | 'unreachable' | 'setup' | 'login' | 'app';
  let phase = $state<Phase>('checking');
  let loginNotice = $state<'expired' | 'loggedOut' | null>(null);
  let username = $state('');
  let loggingOut = $state(false);
  let route = $state(routeFromHash(location.hash));

  function enter(session: SessionInfo): void {
    if (session.state === 'authenticated') {
      username = session.username ?? '';
      phase = 'app';
    } else {
      phase = session.state === 'setup_required' ? 'setup' : 'login';
    }
  }

  async function check(): Promise<void> {
    phase = 'checking';
    const result = await fetchSession();
    if (!result.ok) {
      phase = 'unreachable';
      return;
    }
    enter(result.session);
  }

  function authenticated(session: SessionInfo | null): void {
    loginNotice = null;
    if (session) enter(session);
    else void check();
  }

  async function signOut(): Promise<void> {
    loggingOut = true;
    await logout();
    loggingOut = false;
    loginNotice = 'loggedOut';
    username = '';
    phase = 'login';
  }

  onMount(() => {
    const onHash = () => (route = routeFromHash(location.hash));
    window.addEventListener('hashchange', onHash);
    // A 401 from any request, or the end of the event stream's session, comes
    // back here once; the page that saw it is unmounted with the app.
    const stop = onSessionLost((reason) => {
      username = '';
      if (reason === 'setup_required') {
        phase = 'setup';
      } else {
        loginNotice = 'expired';
        phase = 'login';
      }
    });
    void check();
    return () => {
      window.removeEventListener('hashchange', onHash);
      stop();
    };
  });

  const items: NavItem[] = routes.filter((r) => r.nav !== false).map((r) => ({ id: r.id, href: r.path, label: t[r.labelKey], icon: r.icon }));
  const accountRoute = routes.find((r) => r.id === 'account')!;

  const title = $derived(
    phase === 'app' ? t[route.labelKey] : phase === 'setup' ? t.setupTitle : phase === 'login' ? t.loginTitle : t.brandName,
  );
  $effect(() => {
    document.title = phase === 'app' || phase === 'setup' || phase === 'login' ? `${title} · ${t.brandName}` : t.brandName;
  });
</script>

{#if phase === 'app'}
  <AppShell {items} current={route.id}>
    {#snippet account()}
      <UserMenu username={username || (sessionUser() ?? '')} accountHref={accountRoute.path} current={route.id === 'account'} onLogout={signOut} busy={loggingOut} />
    {/snippet}
    <route.component />
  </AppShell>
{:else}
  <AuthShell>
    {#if phase === 'setup'}
      <Setup onAuthenticated={authenticated} />
    {:else if phase === 'login'}
      <Login notice={loginNotice} onAuthenticated={authenticated} />
    {:else if phase === 'unreachable'}
      <EmptyState icon="warning" tone="warning" title={t.sessionError} description={t.homeLoadErrorHint} role="alert">
        <button type="button" onclick={() => void check()}>{t.homeRetry}</button>
      </EmptyState>
    {:else}
      <EmptyState icon="clock" title={t.sessionLoading} />
    {/if}
  </AuthShell>
{/if}
