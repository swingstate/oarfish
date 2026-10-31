<script lang="ts">
  import { onMount, tick } from 'svelte';
  import FormField from '../components/FormField.svelte';
  import FormMessage from '../components/FormMessage.svelte';
  import PageHeader from '../components/PageHeader.svelte';
  import { errorText, type ErrorField } from '../lib/authErrors';
  import { t } from '../lib/i18n';
  import { login, type SessionInfo } from '../lib/session';

  interface Props {
    /** Why the login page is shown: the session ended, or the user signed out. */
    notice?: 'expired' | 'loggedOut' | null;
    onAuthenticated: (session: SessionInfo | null) => void;
  }
  let { notice = null, onAuthenticated }: Props = $props();

  let username = $state('');
  let password = $state('');
  let busy = $state(false);
  let error = $state<{ text: string; field: ErrorField } | null>(null);
  let errorSeq = $state(0); // a new node per error, so the same sentence is announced again
  let userInput: HTMLInputElement | undefined = $state();
  let passwordInput: HTMLInputElement | undefined = $state();

  onMount(() => userInput?.focus());

  async function submit(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (busy) return;
    if (username === '' || password === '') {
      await fail({ text: t.errorRequired, field: username === '' ? 'username' : 'password' });
      return;
    }
    busy = true;
    const result = await login(username, password);
    password = ''; // never kept after a submit
    busy = false;
    if (result.ok) {
      onAuthenticated(result.session);
      return;
    }
    await fail(errorText(result.code, result.retryAfter));
  }

  async function fail(e: { text: string; field: ErrorField }): Promise<void> {
    error = e;
    errorSeq++;
    await tick();
    (e.field === 'username' ? userInput : passwordInput)?.focus();
  }
</script>

<div data-testid="login">
  <PageHeader title={t.loginTitle} />
  <div class="stack">
    <p>{t.loginIntro}</p>
    {#if notice === 'expired'}
      <FormMessage tone="information" testid="login-notice">{t.loginExpired}</FormMessage>
    {:else if notice === 'loggedOut'}
      <FormMessage tone="information" testid="login-notice">{t.loginLoggedOut}</FormMessage>
    {/if}
    {#key errorSeq}
      {#if error}<FormMessage tone="critical" testid="form-error">{error.text}</FormMessage>{/if}
    {/key}
    <form class="form" novalidate onsubmit={submit}>
      <FormField
        id="login-username"
        label={t.usernameLabel}
        autocomplete="username"
        bind:value={username}
        bind:input={userInput}
        error={error?.field === 'username' ? error.text : null}
        disabled={busy}
      />
      <FormField
        id="login-password"
        type="password"
        label={t.passwordLabel}
        autocomplete="current-password"
        bind:value={password}
        bind:input={passwordInput}
        error={error?.field === 'password' ? error.text : null}
        disabled={busy}
      />
      <div><button type="submit" class="primary" disabled={busy}>{busy ? t.loginWorking : t.loginSubmit}</button></div>
    </form>
  </div>
</div>
