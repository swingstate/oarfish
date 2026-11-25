<script lang="ts">
  import { onMount, tick } from 'svelte';
  import Badge from '../components/Badge.svelte';
  import Card from '../components/Card.svelte';
  import EmptyState from '../components/EmptyState.svelte';
  import FormMessage from '../components/FormMessage.svelte';
  import Notice from '../components/Notice.svelte';
  import PageHeader from '../components/PageHeader.svelte';
  import ActivityPower from './ActivityPower.svelte';
  import { formatDateTime } from '../lib/format';
  import { fmt, t } from '../lib/i18n';
  import type { Tone } from '../lib/ui';
  import {
    fetchAction,
    fetchServices,
    serviceActions,
    submitAction,
    type ServiceAction,
    type ServiceRow,
    type ServicesList,
  } from '../lib/services';
  import { criticalNote, errorText, kindText, resultText, startModeText, stateText } from '../lib/serviceErrors';

  // ADR 0011 B2, B4; docs/UI.md, "Activity page". The list is read on opening,
  // on "Aktualisieren" and once after an action ends: no automatic refresh. The
  // last action is read every 2 s only while one runs; the component's cleanup
  // stops it. The rows and their buttons are a convenience: the broker decides
  // again, and every refusal has its own sentence.
  const pollMs = 2000;
  const pageSize = 100;

  type StateFilter = 'all' | 'active' | 'failed' | 'inactive';
  const stateFilters: StateFilter[] = ['all', 'active', 'failed', 'inactive'];

  let phase = $state<'loading' | 'error' | 'systemd' | 'ready'>('loading');
  let list = $state<ServicesList | null>(null);
  let refreshing = $state(false);
  let loadMessage = $state<{ text: string; seq: number } | null>(null);

  let query = $state('');
  let stateFilter = $state<StateFilter>('all');
  let limit = $state(pageSize);

  // One panel at a time; the row's button gets the focus back when it closes.
  let openRow = $state<string | null>(null);
  let panel: HTMLElement | undefined = $state();
  const rowButtons: Record<string, HTMLButtonElement | undefined> = {};

  // The action that runs, and the one polite live region for it.
  let running = $state<{ unit: string; action: string } | null>(null);
  let submitting = $state(false);
  let live = $state('');

  let alive = true;
  let pollTimer: ReturnType<typeof setTimeout> | undefined;
  let seq = 0;

  // ---- the list ------------------------------------------------------------------

  async function load(): Promise<void> {
    if (list === null) phase = 'loading';
    refreshing = true;
    const r = await fetchServices();
    refreshing = false;
    if (!alive || (!r.ok && r.kind === 'session')) return;
    if (r.ok) {
      list = r.list;
      phase = 'ready';
      loadMessage = null;
      return;
    }
    if (r.kind === 'systemd') {
      list = null;
      phase = 'systemd';
      return;
    }
    if (list === null) {
      phase = 'error';
      return;
    }
    // A refresh failed: the last list stays, the message area says so.
    loadMessage = { text: t.actLoadError, seq: ++seq };
  }

  // ---- the action ------------------------------------------------------------------

  const actionWord = (a: string) => (a === 'start' ? t.actStart : a === 'stop' ? t.actStop : a === 'restart' ? t.actRestart : t.unknownValue);
  const effectText = (a: ServiceAction) => t[`actEffect.${a}`];

  function schedule(): void {
    clearTimeout(pollTimer);
    if (alive && running) pollTimer = setTimeout(() => void poll(), pollMs);
  }

  async function poll(): Promise<void> {
    const r = await fetchAction();
    if (!alive || (!r.ok && r.kind === 'session')) return;
    const a = r.ok ? r.state.action : null;
    if (a && a.step === 'done') {
      running = null;
      live = `${fmt('actResultFor', { action: actionWord(a.action), name: a.unit })} ${resultText(a.code, a.state, a.retry_after_s)}`;
      await load();
      return;
    }
    // Still running, or the answer could not be read this time: ask again.
    schedule();
  }

  async function act(row: ServiceRow, action: ServiceAction): Promise<void> {
    // The buttons are disabled then; this guards the same rule in code.
    if (submitting || running || !list?.broker_available) return;
    submitting = true;
    await closePanel();
    const r = await submitAction(row.name, action);
    submitting = false;
    if (!alive || (!r.ok && r.kind === 'session')) return;
    if (r.ok) {
      running = { unit: row.name, action };
      live = fmt('actRunning', { action: actionWord(action), name: row.name });
      schedule();
      return;
    }
    const prefix = fmt('actResultFor', { action: actionWord(action), name: row.name });
    if (r.kind === 'error') live = `${prefix} ${errorText(r.code, undefined, r.retryAfter)}`;
    else if (r.kind === 'network') live = `${prefix} ${t.errorNetwork}`;
    else if (r.kind === 'systemd') live = `${prefix} ${t.actSystemdMissing}`;
    else live = `${prefix} ${t.errorGeneric}`;
    // The broker's socket may be gone, or the list may be old: read it again once.
    if (r.kind === 'error' && r.code !== 'busy') await load();
  }

  // ---- the panel ---------------------------------------------------------------------

  async function openPanel(name: string): Promise<void> {
    openRow = name;
    await tick();
    panel?.focus();
  }

  async function closePanel(): Promise<void> {
    const name = openRow;
    openRow = null;
    await tick();
    if (name) rowButtons[name]?.focus();
  }

  function togglePanel(name: string): void {
    void (openRow === name ? closePanel() : openPanel(name));
  }

  function onKey(event: KeyboardEvent): void {
    if (event.key === 'Escape' && openRow) {
      event.preventDefault();
      void closePanel();
    }
  }

  onMount(() => {
    void load();
    // An action that runs already (started before the page was opened) is shown and followed.
    void fetchAction().then((r) => {
      if (!alive || !r.ok || r.state.action?.step !== 'running' || running) return;
      const a = r.state.action;
      running = { unit: a.unit, action: a.action };
      live = fmt('actRunning', { action: actionWord(a.action), name: a.unit });
      schedule();
    });
    return () => {
      alive = false;
      clearTimeout(pollTimer);
    };
  });

  // ---- what the list shows ---------------------------------------------------------------

  const stateTone = (s: string): Tone =>
    s === 'active' ? 'success' : s === 'failed' ? 'critical' : s === 'maintenance' ? 'warning' : ['activating', 'deactivating', 'reloading', 'refreshing'].includes(s) ? 'information' : 'neutral';

  const filtered = $derived.by(() => {
    if (!list) return [];
    const q = query.trim().toLowerCase();
    return list.services.filter(
      (r) =>
        (stateFilter === 'all' || r.active_state === stateFilter) &&
        (q === '' || r.name.toLowerCase().includes(q) || r.description.toLowerCase().includes(q)),
    );
  });
  const visible = $derived(filtered.slice(0, limit));
  const offered = (r: ServiceRow) => serviceActions.filter((a) => r.actions.includes(a));
  const actionsLocked = $derived(!list?.broker_available || submitting || running !== null);
</script>

<svelte:window onkeydown={onKey} />

<div data-testid="activity">
  <PageHeader title={t.activityTitle} />

  {#if phase === 'loading'}
    <EmptyState icon="clock" title={t.actLoading} role="status" data-state="loading" />
  {:else if phase === 'systemd'}
    <EmptyState icon="info" title={t.actSystemdMissing} description={t.actSystemdMissingHint} role="status" data-state="systemd" />
  {:else if phase === 'error' || list === null}
    <EmptyState icon="warning" tone="warning" title={t.actLoadError} description={t.actLoadErrorHint} role="alert" data-state="load-error">
      <button type="button" onclick={() => void load()}>{t.dashboardRetry}</button>
    </EmptyState>
  {:else}
    <div class="stack column">
      {#if !list.broker_available}
        <Notice tone="warning" data-testid="act-broker-missing">
          <p>{t['svcErr.unavailable']}</p>
        </Notice>
      {/if}

      <Card id="services" title={t.actServicesHeading} icon="activity">
        <p role="status" class="live" data-testid="act-live">{live}</p>
        {#if loadMessage}
          {#key loadMessage.seq}<FormMessage tone="critical" testid="act-load-message">{loadMessage.text}</FormMessage>{/key}
        {/if}

        <div class="toolbar">
          <button type="button" onclick={() => void load()} disabled={refreshing} data-testid="act-refresh">{refreshing ? t.actRefreshing : t.actRefresh}</button>
          <p class="secondary" data-testid="act-read-at">{fmt('actReadAt', { time: formatDateTime(list.read_at) ?? t.unknownValue })}</p>
        </div>
        <p data-testid="act-counts">{fmt('actCounts', { ...list.counts })}</p>

        <div class="filters">
          <div class="field">
            <label for="act-query">{t.actFilterLabel}</label>
            <input
              id="act-query"
              type="search"
              autocomplete="off"
              autocapitalize="off"
              spellcheck="false"
              bind:value={query}
              oninput={() => (limit = pageSize)}
              data-testid="act-query"
            />
          </div>
          <fieldset class="states">
            <legend>{t.actStateFilterLabel}</legend>
            {#each stateFilters as f (f)}
              <label class="choice">
                <input type="radio" name="act-state" value={f} bind:group={stateFilter} onchange={() => (limit = pageSize)} />
                {t[`actFilter.${f}`]}
              </label>
            {/each}
          </fieldset>
        </div>

        {#if list.services.length === 0}
          <p data-testid="act-empty">{t.actNoServices}</p>
        {:else if filtered.length === 0}
          <p data-testid="act-no-match">{t.actNoMatch}</p>
        {:else}
          {#if filtered.length !== list.services.length || visible.length < filtered.length}
            <p class="secondary" data-testid="act-shown">{fmt('actShown', { shown: visible.length, total: filtered.length })}</p>
          {/if}
          <ul class="rows" data-testid="act-rows">
            {#each visible as r (r.name)}
              {@const note = criticalNote(r.critical)}
              <li class="row" data-testid="act-row" data-name={r.name}>
                <div class="head">
                  <code class="name">{r.name}</code>
                  <Badge tone={stateTone(r.active_state)} data-testid="act-state">{stateText(r.active_state)}</Badge>
                </div>
                <p class="description">{#if r.description}{r.description}{:else}<span class="secondary">{t.actNoDescription}</span>{/if}</p>
                <p class="secondary" data-testid="act-mode">{t.actStartMode}: {startModeText(r.start_mode)}</p>
                {#if r.critical !== ''}<p class="mark" data-testid="act-critical">{fmt('actCriticalRow', { kind: kindText(r.critical) })}</p>{/if}
                {#if r.denied}
                  <p class="mark" data-testid="act-denied">{t.actDeniedRow}. {t.actDeniedNote}</p>
                {:else if r.restart_only}
                  <p class="mark" data-testid="act-restart-only">{t.actRestartOnlyRow}. {t.actRestartOnlyNote}</p>
                {/if}
                {#if !r.denied && offered(r).length > 0}
                  <div>
                    <button
                      type="button"
                      bind:this={rowButtons[r.name]}
                      onclick={() => togglePanel(r.name)}
                      aria-expanded={openRow === r.name}
                      aria-controls={openRow === r.name ? 'act-panel' : undefined}
                      aria-label={fmt('actActionsFor', { name: r.name })}
                      data-testid="act-actions">{t.actActions}</button
                    >
                  </div>
                {/if}
                {#if openRow === r.name}
                  <section class="panel" id="act-panel" tabindex="-1" bind:this={panel} aria-labelledby="act-panel-heading" data-testid="act-panel">
                    <h3 id="act-panel-heading">{fmt('actActionsFor', { name: r.name })}</h3>
                    {#if note}<Notice tone="warning" data-testid="act-critical-note"><p>{note}</p></Notice>{/if}
                    {#if !list.broker_available}<p data-testid="act-panel-unavailable">{t['svcErr.unavailable']}</p>{/if}
                    {#each offered(r) as a (a)}
                      <div class="choice-row">
                        <p>{effectText(a)}</p>
                        <div>
                          <button type="button" class="primary" disabled={actionsLocked} onclick={() => void act(r, a)} data-testid="act-do-{a}">{actionWord(a)}</button>
                        </div>
                      </div>
                    {/each}
                    <div><button type="button" onclick={() => void closePanel()} data-testid="act-close">{t.actClose}</button></div>
                  </section>
                {/if}
              </li>
            {/each}
          </ul>
          {#if visible.length < filtered.length}
            <div><button type="button" onclick={() => (limit += pageSize)} data-testid="act-more">{t.actShowMore}</button></div>
          {/if}
        {/if}
      </Card>
    </div>
  {/if}

  <!-- Reboot and shutdown (G7.5): independent of the service list, so it is there also without systemd or after a load error. -->
  <div class="stack column power">
    <ActivityPower />
  </div>
</div>

<style>
  .column {
    max-width: 56rem;
  }

  .power {
    margin-top: var(--space-5);
  }

  .secondary {
    color: var(--color-text-secondary);
  }

  .live:empty {
    display: none;
  }

  .toolbar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2) var(--space-3);
  }

  .filters {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-end;
    gap: var(--space-3) var(--space-5);
  }

  .field {
    display: grid;
    gap: var(--space-1);
    min-width: 0;
    flex: 1 1 14rem;
  }

  .field input {
    min-height: var(--target-min);
    min-width: 0;
    width: 100%;
  }

  .states {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-1) var(--space-3);
    margin: 0;
    padding: 0;
    border: 0;
    min-width: 0;
  }

  .states legend {
    padding: 0;
    margin-bottom: var(--space-1);
  }

  .choice {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    min-height: var(--target-min);
  }

  .choice input {
    width: 1rem;
    height: 1rem;
    margin: 0;
  }

  .rows {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .row {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: var(--space-1);
    padding: var(--space-3) 0;
    border-top: 1px solid var(--color-border);
  }

  .row:first-child {
    border-top: 0;
  }

  .head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-1) var(--space-3);
    min-width: 0;
  }

  .name {
    font-family: var(--font-mono);
    font-weight: var(--weight-semibold);
    overflow-wrap: anywhere;
    min-width: 0;
  }

  .description {
    overflow-wrap: anywhere;
  }

  .mark {
    font-size: var(--text-sm);
    font-weight: var(--weight-medium);
  }

  .panel {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: var(--space-3);
    margin-top: var(--space-2);
    padding: var(--space-4);
    border: 1px solid var(--color-border-strong);
    border-radius: var(--radius-md);
  }

  .panel h3 {
    font-size: var(--text-md);
    overflow-wrap: anywhere;
  }

  .choice-row {
    display: grid;
    gap: var(--space-1);
  }

  @media (forced-colors: active) {
    .panel,
    .row {
      border-color: CanvasText;
    }
  }
</style>
