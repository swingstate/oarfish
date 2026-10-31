<script lang="ts">
  import { tick } from 'svelte';
  import Card from '../components/Card.svelte';
  import FormField from '../components/FormField.svelte';
  import FormMessage from '../components/FormMessage.svelte';
  import PageHeader from '../components/PageHeader.svelte';
  import { errorText, type ErrorField } from '../lib/authErrors';
  import { fmt, t } from '../lib/i18n';
  import { changePassword, passwordProblem, sessionUser } from '../lib/session';

  const user = sessionUser() ?? '';
  let current = $state('');
  let next = $state('');
  let repeat = $state('');
  let busy = $state(false);
  let done = $state(false);
  let error = $state<{ text: string; field: ErrorField } | null>(null);
  let errorSeq = $state(0);
  let currentInput: HTMLInputElement | undefined = $state();
  let nextInput: HTMLInputElement | undefined = $state();

  async function submit(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (busy) return;
    done = false;
    let problem: { text: string; field: ErrorField } | null = null;
    if (current === '') problem = { text: t.errorRequired, field: 'current' };
    else if (passwordProblem(next)) problem = errorText(passwordProblem(next)!);
    else if (next !== repeat) problem = errorText('password_mismatch');
    if (problem) {
      clear();
      await fail(problem);
      return;
    }
    busy = true;
    const result = await changePassword(current, next);
    clear();
    busy = false;
    if (result.ok) {
      error = null;
      done = true;
      return;
    }
    await fail(errorText(result.code, result.retryAfter));
  }

  function clear(): void {
    current = '';
    next = '';
    repeat = '';
  }

  async function fail(e: { text: string; field: ErrorField }): Promise<void> {
    error = e;
    errorSeq++;
    await tick();
    (e.field === 'password' ? nextInput : currentInput)?.focus();
  }
</script>

<div data-testid="account">
  <PageHeader title={t.accountTitle} />
  <div class="stack">
    <p data-testid="account-user">{fmt('accountSignedInAs', { user })}</p>
    <p class="note">{t.accountSessionNote}</p>
    <Card id="password" title={t.accountPasswordHeading} icon="lock">
      <div class="stack">
        {#key errorSeq}
          {#if error}<FormMessage tone="critical" testid="form-error">{error.text}</FormMessage>{/if}
        {/key}
        {#if done}<FormMessage tone="success" testid="account-done">{t.accountChanged}</FormMessage>{/if}
        <form class="form" novalidate onsubmit={submit}>
          <FormField
            id="account-current"
            type="password"
            label={t.currentPasswordLabel}
            autocomplete="current-password"
            bind:value={current}
            bind:input={currentInput}
            error={error?.field === 'current' ? error.text : null}
            disabled={busy}
          />
          <FormField
            id="account-new"
            type="password"
            label={t.newPasswordLabel}
            autocomplete="new-password"
            help={t.newPasswordHelp}
            bind:value={next}
            bind:input={nextInput}
            error={error?.field === 'password' ? error.text : null}
            disabled={busy}
          />
          <FormField id="account-repeat" type="password" label={t.newPasswordRepeatLabel} autocomplete="new-password" bind:value={repeat} disabled={busy} />
          <div><button type="submit" class="primary" disabled={busy}>{busy ? t.accountWorking : t.accountSubmit}</button></div>
        </form>
      </div>
    </Card>
  </div>
</div>

<style>
  .note {
    color: var(--color-text-secondary);
  }
</style>
