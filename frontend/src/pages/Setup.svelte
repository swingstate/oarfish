<script lang="ts">
  import { onMount, tick } from 'svelte';
  import FormField from '../components/FormField.svelte';
  import FormMessage from '../components/FormMessage.svelte';
  import Icon from '../components/Icon.svelte';
  import PageHeader from '../components/PageHeader.svelte';
  import { errorText, type ErrorField } from '../lib/authErrors';
  import { t } from '../lib/i18n';
  import { passwordProblem, setup, usernamePattern, type SessionInfo } from '../lib/session';

  interface Props {
    onAuthenticated: (session: SessionInfo | null) => void;
  }
  let { onAuthenticated }: Props = $props();

  let token = $state('');
  let username = $state('');
  let password = $state('');
  let repeat = $state('');
  let busy = $state(false);
  let error = $state<{ text: string; field: ErrorField } | null>(null);
  let errorSeq = $state(0);
  let tokenInput: HTMLInputElement | undefined = $state();
  let userInput: HTMLInputElement | undefined = $state();
  let passwordInput: HTMLInputElement | undefined = $state();

  onMount(() => tokenInput?.focus());

  // Checks the server repeats; here they give a specific answer at once.
  function localProblem(): { text: string; field: ErrorField } | null {
    if (token.trim() === '') return { text: t.errorRequired, field: 'token' };
    if (!usernamePattern.test(username)) return errorText('username_invalid');
    const p = passwordProblem(password);
    if (p) return errorText(p);
    if (password !== repeat) return errorText('password_mismatch');
    return null;
  }

  async function submit(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (busy) return;
    const problem = localProblem();
    if (problem) {
      clearPasswords();
      await fail(problem);
      return;
    }
    busy = true;
    const result = await setup(token, username, password);
    clearPasswords();
    busy = false;
    if (result.ok) {
      token = '';
      onAuthenticated(result.session);
      return;
    }
    await fail(errorText(result.code, result.retryAfter));
  }

  function clearPasswords(): void {
    password = '';
    repeat = '';
  }

  async function fail(e: { text: string; field: ErrorField }): Promise<void> {
    error = e;
    errorSeq++;
    await tick();
    ({ token: tokenInput, username: userInput, password: passwordInput, current: passwordInput })[e.field ?? 'token']?.focus();
  }
</script>

<div data-testid="setup">
  <PageHeader title={t.setupTitle} />
  <div class="stack">
    <p>{t.setupIntro}</p>
    {#key errorSeq}
      {#if error}<FormMessage tone="critical" testid="form-error">{error.text}</FormMessage>{/if}
    {/key}
    <form class="form" novalidate onsubmit={submit}>
      <FormField
        id="setup-token"
        label={t.setupTokenLabel}
        autocomplete="off"
        help={t.setupTokenHelp}
        bind:value={token}
        bind:input={tokenInput}
        error={error?.field === 'token' ? error.text : null}
        disabled={busy}
      />
      <p class="command"><code data-testid="setup-command">{t.setupTokenCommand}</code></p>
      <FormField
        id="setup-username"
        label={t.usernameLabel}
        autocomplete="username"
        help={t.usernameHelp}
        bind:value={username}
        bind:input={userInput}
        error={error?.field === 'username' ? error.text : null}
        disabled={busy}
      />
      <FormField
        id="setup-password"
        type="password"
        label={t.passwordLabel}
        autocomplete="new-password"
        help={t.newPasswordHelp}
        bind:value={password}
        bind:input={passwordInput}
        error={error?.field === 'password' ? error.text : null}
        disabled={busy}
      />
      <FormField id="setup-repeat" type="password" label={t.passwordRepeatLabel} autocomplete="new-password" bind:value={repeat} disabled={busy} />
      <div><button type="submit" class="primary" disabled={busy}>{busy ? t.setupWorking : t.setupSubmit}</button></div>
    </form>
    <section class="binding" aria-labelledby="setup-binding-heading">
      <h2 id="setup-binding-heading"><Icon name="lock" /> {t.setupBindingHeading}</h2>
      <p data-testid="setup-binding">{t.setupBinding}</p>
    </section>
  </div>
</div>

<style>
  .command code {
    display: block;
    padding: var(--space-2) var(--space-3);
    background: var(--color-surface-muted);
    border-radius: var(--radius-md);
  }

  .binding h2 {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    font-size: var(--text-lg);
    margin-bottom: var(--space-1);
  }

  .binding p {
    color: var(--color-text-secondary);
  }
</style>
