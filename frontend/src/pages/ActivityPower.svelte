<script lang="ts">
  import { onMount, tick } from 'svelte';
  import Card from '../components/Card.svelte';
  import Notice from '../components/Notice.svelte';
  import { t } from '../lib/i18n';
  import { fetchPower, powerAccepted, requestPower, type PowerAction, type WaitingItem } from '../lib/power';
  import { powerErrorText, waitingText } from '../lib/powerErrors';

  // ADR 0011 B4, B6; docs/UI.md, "Restart and shutdown". The card reads GET
  // /api/v1/power once on opening (for the broker's socket) and once each time
  // the dialog opens (for the waiting actions). The dialog is a native modal
  // dialog: the browser keeps the focus inside it; the focus goes to its title
  // on opening and back to the button that opened it on closing; Escape and
  // "Abbrechen" close it while no request runs. The password is only in the
  // input element and in the one request: the code reads it from the element
  // when the form is sent and clears the element on close, on error and on
  // success. No variable, store, log or URL holds it.

  let brokerAvailable = $state<boolean | null>(null);

  let dialog: HTMLDialogElement | undefined = $state();
  let titleEl: HTMLElement | undefined = $state();
  let input: HTMLInputElement | undefined = $state();
  const openers: Partial<Record<PowerAction, HTMLButtonElement>> = $state({});

  let action = $state<PowerAction | null>(null);
  let waiting = $state<{ state: 'loading' } | { state: 'error' } | { state: 'ready'; items: WaitingItem[] }>({ state: 'loading' });
  let hasPassword = $state(false);
  let sending = $state(false);
  let error = $state<{ text: string; field: 'password' | null; seq: number } | null>(null);
  let seq = 0;
  let alive = true;
  let openSeq = 0;

  const title = $derived(action === 'poweroff' ? t.pwrPoweroffTitle : t.pwrRebootTitle);
  const body = $derived(action === 'poweroff' ? t.pwrPoweroffBody : t.pwrRebootBody);
  const confirmLabel = $derived(action === 'poweroff' ? t.pwrPoweroff : t.pwrReboot);

  function clearPassword(): void {
    if (input) input.value = '';
    hasPassword = false;
  }

  async function readWaiting(n: number): Promise<void> {
    waiting = { state: 'loading' };
    const r = await fetchPower();
    if (!alive || n !== openSeq) return;
    if (!r.ok) {
      waiting = { state: 'error' };
      return;
    }
    brokerAvailable = r.state.broker_available;
    waiting = { state: 'ready', items: r.state.waiting };
  }

  async function openDialog(a: PowerAction): Promise<void> {
    if (action !== null || brokerAvailable === false) return;
    action = a;
    error = null;
    clearPassword();
    const n = ++openSeq;
    await tick();
    if (dialog && typeof dialog.showModal === 'function') dialog.showModal();
    else dialog?.setAttribute('open', '');
    titleEl?.focus();
    void readWaiting(n);
  }

  async function closeDialog(): Promise<void> {
    if (action === null || sending) return;
    const opener = openers[action];
    clearPassword();
    error = null;
    action = null;
    openSeq++;
    if (dialog && typeof dialog.close === 'function') dialog.close();
    else dialog?.removeAttribute('open');
    await tick();
    opener?.focus();
  }

  function onDialogKey(event: KeyboardEvent): void {
    if (event.key === 'Escape') {
      event.preventDefault();
      void closeDialog();
    }
  }

  function onCancel(event: Event): void {
    // The browser's own Escape: closed the same way, or refused while a request runs.
    event.preventDefault();
    void closeDialog();
  }

  async function submit(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (action === null || sending || !input || input.value === '') return;
    const a = action;
    sending = true;
    error = null;
    const r = await requestPower(a, input.value);
    clearPassword();
    sending = false;
    if (!alive || (!r.ok && r.kind === 'session')) return;
    if (r.ok) {
      action = null;
      if (dialog && typeof dialog.close === 'function') dialog.close();
      else dialog?.removeAttribute('open');
      powerAccepted(a);
      return;
    }
    const e = powerErrorText(r);
    error = { ...e, seq: ++seq };
    if (r.kind === 'error' && r.code === 'unavailable') brokerAvailable = false;
    await tick();
    input?.focus();
  }

  onMount(() => {
    void fetchPower().then((r) => {
      if (alive && r.ok) brokerAvailable = r.state.broker_available;
    });
    return () => {
      alive = false;
      openSeq++;
      clearPassword();
    };
  });

  const describedBy = $derived(['pwr-password-help', error ? 'pwr-error' : null].filter(Boolean).join(' '));
</script>

<Card id="power" title={t.pwrHeading} icon="power" data-testid="pwr-card">
  <p>{t.pwrIntro}</p>
  {#if brokerAvailable === false}
    <Notice tone="warning" data-testid="pwr-broker-missing"><p>{t['pwrErr.unavailable']}</p></Notice>
  {/if}
  <div class="buttons">
    <button type="button" bind:this={openers.reboot} disabled={brokerAvailable === false || action !== null} onclick={() => void openDialog('reboot')} data-testid="pwr-open-reboot"
      >{t.pwrReboot}</button
    >
    <button type="button" bind:this={openers.poweroff} disabled={brokerAvailable === false || action !== null} onclick={() => void openDialog('poweroff')} data-testid="pwr-open-poweroff"
      >{t.pwrPoweroff}</button
    >
  </div>
</Card>

<dialog bind:this={dialog} aria-labelledby="pwr-dialog-title" onkeydown={onDialogKey} oncancel={onCancel} data-testid="pwr-dialog">
  {#if action !== null}
    <div class="content">
      <h2 id="pwr-dialog-title" tabindex="-1" bind:this={titleEl}>{title}</h2>
      <p data-testid="pwr-body">{body}</p>
      <p data-testid="pwr-tunnel">{t.pwrTunnel}</p>
      <section aria-labelledby="pwr-waiting-heading" data-testid="pwr-waiting">
        <h3 id="pwr-waiting-heading">{t.pwrWaitingHeading}</h3>
        {#if waiting.state === 'loading'}
          <p class="secondary" data-state="loading">{t.pwrWaitingLoading}</p>
        {:else if waiting.state === 'error'}
          <p data-state="error">{t.pwrWaitingError}</p>
        {:else if waiting.items.length === 0}
          <p data-state="none">{t.pwrWaitingNone}</p>
        {:else}
          <ul data-state="list">
            {#each waiting.items as item, i (i)}<li data-kind={item.kind}>{waitingText(item)}</li>{/each}
          </ul>
        {/if}
      </section>
      <form novalidate onsubmit={(e) => void submit(e)}>
        <div class="field">
          <label for="pwr-password">{t.updPasswordLabel}</label>
          <p class="help" id="pwr-password-help">{t.pwrPasswordHelp}</p>
          <input
            id="pwr-password"
            name="pwr-password"
            type="password"
            autocomplete="current-password"
            autocapitalize="off"
            spellcheck="false"
            required
            bind:this={input}
            oninput={() => (hasPassword = !!input && input.value !== '')}
            disabled={sending}
            aria-invalid={error?.field === 'password' ? 'true' : undefined}
            aria-describedby={describedBy}
            data-testid="pwr-password"
          />
          {#if error}
            {#key error.seq}<p class="error" id="pwr-error" role="alert" data-testid="pwr-error">{error.text}</p>{/key}
          {/if}
        </div>
        <div class="buttons">
          <button type="button" onclick={() => void closeDialog()} disabled={sending} data-testid="pwr-cancel">{t.updCancel}</button>
          <button type="submit" class="primary" disabled={!hasPassword || sending} data-testid="pwr-confirm">{sending ? t.pwrWorking : confirmLabel}</button>
        </div>
      </form>
    </div>
  {/if}
</dialog>

<style>
  .buttons {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2) var(--space-3);
  }

  dialog {
    width: min(36rem, calc(100vw - 2 * var(--space-4)));
    max-height: calc(100vh - 2 * var(--space-4));
    padding: 0;
    color: var(--color-text);
    background: var(--color-surface);
    border: 1px solid var(--color-border-strong);
    border-radius: var(--radius-md);
  }

  .content {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: var(--space-3);
    padding: var(--space-5);
  }

  h2 {
    font-size: var(--text-lg);
  }

  h3 {
    font-size: var(--text-md);
  }

  ul {
    margin: 0;
    padding-left: var(--space-5);
  }

  .secondary,
  .help {
    color: var(--color-text-secondary);
  }

  form {
    display: grid;
    gap: var(--space-3);
  }

  .field {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: var(--space-1);
  }

  label {
    font-weight: var(--weight-medium);
  }

  .help {
    font-size: var(--text-sm);
  }

  input {
    min-height: 2.5rem;
    padding: var(--space-2) var(--space-3);
    font: inherit;
    color: var(--color-text);
    background: var(--color-surface);
    border: 1px solid var(--color-border-strong);
    border-radius: var(--radius-md);
  }

  input[aria-invalid='true'] {
    border-color: var(--color-critical-border);
    border-width: 2px;
  }

  input:disabled {
    color: var(--color-text-disabled);
    background: var(--color-surface-muted);
    cursor: not-allowed;
  }

  .error {
    font-size: var(--text-sm);
    color: var(--color-critical-fg);
    font-weight: var(--weight-medium);
  }

  @media (forced-colors: active) {
    dialog,
    input {
      border-color: CanvasText;
    }
  }
</style>
