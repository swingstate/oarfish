<script lang="ts">
  import { onMount, tick } from 'svelte';
  import Badge from '../components/Badge.svelte';
  import Card from '../components/Card.svelte';
  import EmptyState from '../components/EmptyState.svelte';
  import FormField from '../components/FormField.svelte';
  import FormMessage from '../components/FormMessage.svelte';
  import Icon from '../components/Icon.svelte';
  import KeyValueList from '../components/KeyValueList.svelte';
  import Notice from '../components/Notice.svelte';
  import PageHeader from '../components/PageHeader.svelte';
  import { formatBytes, formatDateTime } from '../lib/format';
  import { fmt, t } from '../lib/i18n';
  import type { Tone } from '../lib/ui';
  import {
    applyUpdate,
    checkForUpdate,
    discardUpdate,
    fetchUpdateState,
    outcomes,
    type JobStep,
    type UpdateFailure,
    type UpdateState,
  } from '../lib/update';
  import { actionErrorText, durationText, updateErrorText, type UpdateErrorField } from '../lib/updateErrors';

  // ADR 0010 U3, U9, A11.3. Polling runs only while a job runs or while the page
  // waits for the restart after "activating", every 2 s, and stops when the page
  // closes. The page checks on its own only when the State says a check is due
  // (never checked, or next_check_at has passed); "Jetzt prüfen" is locked for 60 s.
  const pollMs = 2000;
  const restartLimitMs = 3 * 60_000;
  const checkLockMs = 60_000;
  // Phases of the helper in which a transaction is under way (U5, U6).
  const transient = new Set(['staging', 'switching', 'verifying', 'rolling_back']);
  const applySteps: JobStep[] = ['fetching_metadata', 'downloading', 'staging', 'activating', 'done'];

  let phase = $state<'loading' | 'error' | 'ready'>('loading');
  let st = $state<UpdateState | null>(null);

  // "Jetzt prüfen"
  let checking = $state(false);
  let lockUntil = $state(0);
  let now = $state(Date.now());
  let checkMessage = $state<{ text: string; tone: Tone; seq: number } | null>(null);

  // The confirmation. The password is only in the input and in the request.
  let confirmOpen = $state(false);
  let password = $state('');
  let busy = $state(false);
  let formError = $state<{ text: string; field: UpdateErrorField } | null>(null);
  let errorSeq = $state(0);
  let passwordInput: HTMLInputElement | undefined = $state();
  let applyButton: HTMLButtonElement | undefined = $state();

  // Progress and restart.
  let watching = $state(false);
  let restart = $state<'none' | 'restarting' | 'timeout'>('none');
  let deadline = 0;
  let lastStep = $state<JobStep | null>(null);

  // Discard.
  let discarding = $state(false);
  let discardMessage = $state<{ text: string; tone: Tone; seq: number } | null>(null);

  let alive = true;
  let pollTimer: ReturnType<typeof setTimeout> | undefined;
  let tickTimer: ReturnType<typeof setInterval> | undefined;
  let seq = 0;

  // ---- reading the State ------------------------------------------------------

  async function load(first: boolean): Promise<void> {
    if (st === null) phase = 'loading';
    const r = await fetchUpdateState();
    if (!alive || (!r.ok && r.kind === 'session')) return;
    if (!r.ok) {
      phase = 'error';
      return;
    }
    adopt(r.state);
    phase = 'ready';
    if (first && checkDue(r.state)) await runCheck(false);
    schedule();
  }

  /** The page's own check (ADR 0010 U3): only when nothing is running and the State says it is due. */
  function checkDue(s: UpdateState): boolean {
    if (s.job?.running) return false;
    if (s.check.state === 'never') return true;
    const next = s.check.next_check_at ? Date.parse(s.check.next_check_at) : NaN;
    return !Number.isNaN(next) && next <= Date.now();
  }

  function adopt(s: UpdateState): void {
    st = s;
    if (restart === 'restarting') restart = 'none';
    const limit = s.check.error?.code === 'rate_limited' ? s.check.error.retry_after_seconds : undefined;
    if (limit) lock(limit * 1000);
    const j = s.job;
    // A running apply job, or one whose activate was accepted while the restart is still to come.
    if (j?.kind === 'apply' && (j.running || (j.step === 'done' && !settled(s)))) watching = true;
    if (j?.kind === 'apply' && j.step !== 'failed') lastStep = j.step;
    if (watching && (j?.step === 'activating' || j?.step === 'done') && deadline === 0) deadline = Date.now() + restartLimitMs;
    if (watching && settled(s)) {
      watching = false;
      deadline = 0;
    }
  }

  /** Whether the update the page watches has come to an end that the State can show. */
  function settled(s: UpdateState): boolean {
    const j = s.job;
    const underWay = s.helper.present && transient.has(s.helper.phase ?? '');
    if (j?.running) return false;
    if (j?.kind === 'apply' && j.step === 'failed') return true;
    // "done" in the old process: activate was accepted and the restart is still to come.
    if (j?.kind === 'apply' && j.step === 'done') return s.last_result?.to === j.version && !underWay;
    return !underWay;
  }

  function shouldPoll(): boolean {
    return restart === 'restarting' || watching || !!st?.job?.running;
  }

  function schedule(): void {
    clearTimeout(pollTimer);
    if (alive && shouldPoll()) pollTimer = setTimeout(() => void poll(), pollMs);
  }

  async function poll(): Promise<void> {
    const r = await fetchUpdateState();
    if (!alive || (!r.ok && r.kind === 'session')) return; // a 401 goes to the login page
    if (r.ok) {
      adopt(r.state);
      phase = 'ready';
    } else if (watching) {
      // Around "activating" the service restarts: no answer is expected for a while.
      restart = 'restarting';
      if (deadline === 0) deadline = Date.now() + restartLimitMs;
    } else {
      // A check job whose state can no longer be read: the page says so and stops.
      phase = 'error';
      return;
    }
    if ((watching || restart === 'restarting') && deadline > 0 && Date.now() > deadline) {
      restart = 'timeout';
      watching = false;
      return;
    }
    schedule();
  }

  async function refresh(): Promise<void> {
    const r = await fetchUpdateState();
    if (alive && r.ok) adopt(r.state);
    schedule();
  }

  // ---- "Jetzt prüfen" -----------------------------------------------------------

  function lock(ms: number): void {
    lockUntil = Math.max(lockUntil, Date.now() + ms);
    now = Date.now();
    if (tickTimer === undefined) {
      tickTimer = setInterval(() => {
        now = Date.now();
        if (now >= lockUntil) {
          clearInterval(tickTimer);
          tickTimer = undefined;
        }
      }, 1000);
    }
  }

  async function runCheck(force: boolean): Promise<void> {
    checking = true;
    checkMessage = null;
    if (force) lock(checkLockMs);
    const r = await checkForUpdate(force);
    checking = false;
    if (!alive) return;
    if (r.ok) {
      adopt(r.state);
      if (force && r.state.check.throttled) checkMessage = { text: t.updThrottled, tone: 'information', seq: ++seq };
    } else if (r.kind !== 'session') {
      if (r.kind === 'error' && (r.code === 'rate_limited' || r.code === 'cooldown')) lock((r.retryAfter ?? 60) * 1000);
      checkMessage = { text: failureText(r), tone: r.kind === 'error' && r.code === 'busy' ? 'information' : 'critical', seq: ++seq };
      await refresh();
    }
    schedule();
  }

  function failureText(r: UpdateFailure): string {
    if (r.kind === 'error') return actionErrorText(r.code, r.retryAfter).text;
    return r.kind === 'network' ? t.errorNetwork : t.errorGeneric;
  }

  const lockLeft = $derived(Math.max(0, Math.ceil((lockUntil - now) / 1000)));

  // ---- apply, second confirmation, discard ----------------------------------

  async function openConfirm(): Promise<void> {
    confirmOpen = true;
    formError = null;
    await tick();
    passwordInput?.focus();
  }

  async function closeConfirm(): Promise<void> {
    confirmOpen = false;
    password = '';
    formError = null;
    await tick();
    applyButton?.focus();
  }

  function onConfirmKey(event: KeyboardEvent): void {
    if (event.key === 'Escape' && confirmOpen && !busy) {
      event.preventDefault();
      void closeConfirm();
    }
  }

  async function submitApply(event: SubmitEvent, version: string, components: boolean): Promise<void> {
    event.preventDefault();
    if (busy) return;
    if (password === '') {
      await fail({ text: t.errorRequired, field: 'password' });
      return;
    }
    busy = true;
    const r = await applyUpdate(version, password, components);
    password = '';
    if (passwordInput) passwordInput.value = '';
    busy = false;
    if (!alive || (!r.ok && r.kind === 'session')) return;
    if (r.ok) {
      formError = null;
      confirmOpen = false;
      watching = true;
      deadline = 0;
      restart = 'none';
      await refresh();
      await tick();
      document.getElementById('progress-heading')?.closest('section')?.focus();
      return;
    }
    await fail(r.kind === 'error' ? actionErrorText(r.code, r.retryAfter) : { text: failureText(r), field: null });
    if (r.kind === 'error' && r.code !== 'wrong_password' && r.code !== 'rate_limited') await refresh();
  }

  async function fail(e: { text: string; field: UpdateErrorField }): Promise<void> {
    formError = e;
    errorSeq++;
    await tick();
    passwordInput?.focus();
  }

  async function discard(version: string): Promise<void> {
    if (discarding) return;
    discarding = true;
    discardMessage = null;
    const r = await discardUpdate(version);
    discarding = false;
    if (!alive || (!r.ok && r.kind === 'session')) return;
    discardMessage = r.ok ? { text: t.updDiscarded, tone: 'success', seq: ++seq } : { text: failureText(r), tone: 'critical', seq: ++seq };
    await refresh();
  }

  onMount(() => {
    void load(true);
    return () => {
      alive = false;
      clearTimeout(pollTimer);
      clearInterval(tickTimer);
    };
  });

  // ---- what the State means for the page ----------------------------------------

  const job = $derived(st?.job ?? null);
  const applyJob = $derived(job?.kind === 'apply' ? job : null);
  const checkRunning = $derived(checking || (job?.kind === 'check' && job.running));
  const awaiting = $derived(applyJob?.running && applyJob.step === 'awaiting_components_confirmation' ? applyJob : null);
  const showProgress = $derived(restart !== 'none' || (applyJob !== null && !awaiting && (applyJob.running || watching)));
  const installer = $derived(st?.installer?.required ? st.installer : null);
  const failedJob = $derived(applyJob && !applyJob.running && applyJob.step === 'failed' && !installer ? applyJob : null);
  const helper = $derived(st?.helper ?? null);
  const stagedVersion = $derived(helper?.staged_version ?? (helper?.phase === 'staged' ? (applyJob?.version ?? null) : null));
  const cooldownLeft = $derived(helper?.cooldown_remaining_s ?? 0);

  function inCooldown(version: string): boolean {
    return cooldownLeft > 0 && (!helper?.cooldown_version || helper.cooldown_version === version);
  }

  const canApply = $derived.by(() => {
    const a = st?.available;
    if (!a?.verified || !helper?.present || job?.running || watching || restart !== 'none' || stagedVersion) return false;
    return !inCooldown(a.version) && !(installer && installer.version === a.version);
  });

  const steps = $derived.by<JobStep[]>(() => {
    const cur = applyJob?.step ?? lastStep;
    return cur === 'awaiting_components_confirmation' ? [...applySteps.slice(0, 3), cur, ...applySteps.slice(3)] : applySteps;
  });

  /** The step marked as current: "done" in the old process still means the switch and the restart are under way. */
  const currentStep = $derived.by<JobStep>(() => {
    const s = restart !== 'none' ? (lastStep ?? 'activating') : (applyJob?.step ?? lastStep ?? 'fetching_metadata');
    return s === 'done' || s === 'failed' ? 'activating' : s;
  });

  const currentIndex = $derived(steps.indexOf(currentStep));
  const stepText = (s: JobStep) => t[`updStep.${s}`];

  const outcomeTone: Record<string, Tone> = {
    success: 'success',
    rolled_back: 'warning',
    components_rolled_back: 'warning',
    rollback_failed: 'critical',
    aborted: 'neutral',
  };
  const knownOutcome = (o: string): o is (typeof outcomes)[number] => (outcomes as readonly string[]).includes(o);
</script>

<svelte:window onkeydown={onConfirmKey} />

<div data-testid="update">
  <PageHeader title={t.updateTitle} />

  {#if phase === 'loading'}
    <EmptyState icon="clock" title={t.updLoading} role="status" data-state="loading" />
  {:else if phase === 'error' || st === null}
    <EmptyState icon="warning" tone="warning" title={t.updLoadError} description={t.dashboardLoadErrorHint} role="alert" data-state="load-error">
      <button type="button" onclick={() => void load(false)}>{t.dashboardRetry}</button>
    </EmptyState>
  {:else}
    <div class="stack column">
      {#if !st.helper.present}
        <Notice tone="warning" data-testid="upd-helper-missing">
          <p>{t.updHelperMissing}</p>
          <p class="secondary">{t.updHelperMissingHint}</p>
          {#if st.helper.error}<p class="secondary">{updateErrorText(st.helper.error.code)}</p>{/if}
        </Notice>
      {/if}

      {#if st.helper.needs_attention}
        <Notice tone="critical" data-testid="upd-needs-attention">
          <p>{t.updNeedsAttention}</p>
          <p class="secondary">{t.updRerunHint}</p>
        </Notice>
      {/if}

      {#if cooldownLeft > 0}
        <Notice tone="information" data-testid="upd-cooldown">
          <p>
            {#if st.helper.cooldown_version}
              {fmt('updCooldown', { version: st.helper.cooldown_version, duration: durationText(cooldownLeft) })}
            {:else}
              {fmt('updCooldownAny', { duration: durationText(cooldownLeft) })}
            {/if}
          </p>
        </Notice>
      {/if}

      {#if stagedVersion && !awaiting}
        <Notice tone="information" data-testid="upd-staged">
          <p>{fmt('updStaged', { version: stagedVersion })}</p>
          <div><button type="button" onclick={() => void discard(stagedVersion)} disabled={discarding}>{discarding ? t.updDiscardWorking : t.updDiscard}</button></div>
        </Notice>
      {/if}
      {#if discardMessage}
        {#key discardMessage.seq}<FormMessage tone={discardMessage.tone} testid="upd-discard-message">{discardMessage.text}</FormMessage>{/key}
      {/if}

      {#if installer}
        <Notice tone="warning" data-testid="upd-installer">
          <p><strong>{t.updInstallerTitle}.</strong> {t.updInstallerText}</p>
          <p>{fmt('updInstallerSteps', { version: installer.version })}</p>
          <p><code>{t.updInstallerCommand}</code></p>
          <p>{t.updInstallerKept}</p>
          {#if installer.differs.length > 0}
            <p>{t.updInstallerDiffers}</p>
            <ul class="names">
              {#each installer.differs as name (name)}<li><code>{name}</code></li>{/each}
            </ul>
          {/if}
        </Notice>
      {/if}

      <div class="stack">
        <Card id="version" title={t.updVersionHeading} icon="update">
          <KeyValueList
            items={[
              { label: t.updInstalledVersion, value: st.installed_version || null, mono: true, testid: 'upd-installed', placeholder: t.unknownValue },
              { label: t.updCheckStatus, value: checkRunning ? t.updChecking : t[`updCheckState.${st.check.state}`], testid: 'upd-check-state' },
              { label: t.updCheckedAt, value: formatDateTime(st.check.checked_at), placeholder: t.updNever, testid: 'upd-checked-at' },
              { label: t.updNextCheckAt, value: formatDateTime(st.check.next_check_at), testid: 'upd-next-check' },
            ]}
          />
          {#if checkRunning}
            <p role="status" data-testid="upd-check-running">{t.updChecking}</p>
          {:else if st.check.state === 'never'}
            <p data-testid="upd-never">{t.updNeverChecked}</p>
          {:else if st.check.state === 'error' && st.check.error}
            <Notice tone="warning" data-testid="upd-check-error">
              <p><strong>{t.updCheckFailed}</strong> {updateErrorText(st.check.error.code)}</p>
              {#if st.check.error.retry_after_seconds}
                <p class="secondary">{fmt('updRetryHint', { duration: durationText(st.check.error.retry_after_seconds) })}</p>
              {/if}
            </Notice>
          {:else if st.check.state === 'ok' && st.available === null}
            <p class="calm" data-testid="upd-up-to-date"><Icon name="success" />{t.updUpToDate}</p>
          {/if}
          {#if checkMessage}
            {#key checkMessage.seq}<FormMessage tone={checkMessage.tone} testid="upd-check-message">{checkMessage.text}</FormMessage>{/key}
          {/if}
          <div class="actions">
            <button type="button" onclick={() => void runCheck(true)} disabled={checkRunning || lockLeft > 0 || !!job?.running || watching || restart !== 'none'} aria-describedby="upd-check-note" data-testid="upd-check-now">
              {checkRunning ? t.updChecking : t.updCheckNow}
            </button>
            {#if lockLeft > 0}<p class="secondary" data-testid="upd-check-lock">{fmt('updCheckLocked', { duration: durationText(lockLeft) })}</p>{/if}
          </div>
          <p class="secondary" id="upd-check-note">{t.updCheckNote}</p>
        </Card>

        {#if showProgress}
          <Card id="progress" title={t.updProgressHeading} icon="clock" tabindex={-1} data-testid="upd-progress">
            <ol class="steps" aria-label={t.updStepsLabel}>
              {#each steps as s, i (s)}
                {@const cur = currentIndex}
                <li data-status={i < cur ? 'done' : i === cur ? 'current' : 'pending'} aria-current={i === cur ? 'step' : undefined}>
                  <span class="mark">{#if i < cur}<Icon name="success" />{:else if i === cur}<Icon name="clock" />{/if}</span>
                  <span class="name">{stepText(s)}</span>
                  <span class="state">{i < cur ? t.updStepDone : i === cur ? t.updStepCurrent : t.updStepPending}</span>
                </li>
              {/each}
            </ol>
            <p role="status" class="live" data-testid="upd-live">
              {#if restart === 'restarting'}
                {lastStep === 'activating' || lastStep === 'done' ? t.updRestarting : t.updNoAnswer}
              {:else}
                {fmt('updCurrentStep', { step: stepText(currentStep) })}
              {/if}
            </p>
            {#if restart === 'timeout'}
              <Notice tone="warning" data-testid="upd-restart-timeout">
                <p>{t.updRestartTimeout}</p>
                <div><button type="button" onclick={() => { restart = 'none'; void load(false); }}>{t.deviceReload}</button></div>
              </Notice>
            {/if}
          </Card>
        {/if}

        {#if failedJob?.error}
          <Card id="failed" title={t.updProgressHeading} icon="clock" data-testid="upd-job-failed">
            <Notice tone="critical">
              <p><strong>{fmt('updJobFailed', { version: failedJob.version ?? '' })}</strong> {updateErrorText(failedJob.error.code)}</p>
            </Notice>
          </Card>
        {/if}

        {#if awaiting}
          <Card id="components" title={t.updComponentsHeading} icon="lock" data-testid="upd-components">
            <p>{t.updComponentsText}</p>
            <p>{t.updConfirmRestart}</p>
            <p>{t.updComponentsFail}</p>
            {#key errorSeq}
              {#if formError}<FormMessage tone="critical" testid="form-error">{formError.text}</FormMessage>{/if}
            {/key}
            <form class="form" novalidate onsubmit={(e) => void submitApply(e, awaiting.version ?? '', true)}>
              <FormField
                id="upd-components-password"
                type="password"
                label={t.updPasswordLabel}
                autocomplete="current-password"
                bind:value={password}
                bind:input={passwordInput}
                error={formError?.field === 'password' ? formError.text : null}
                disabled={busy}
              />
              <div class="buttons">
                <button type="submit" class="primary" disabled={busy}>{busy ? t.updConfirmWorking : t.updComponentsSubmit}</button>
                <button type="button" onclick={() => void discard(awaiting.version ?? '')} disabled={busy || discarding}>{t.updDiscard}</button>
              </div>
            </form>
          </Card>
        {/if}

        {#if st.last_result && !watching && restart === 'none'}
          {@const r = st.last_result}
          <Card id="result" title={t.updResultHeading} icon="info" data-testid="upd-result">
            <div><Badge tone={outcomeTone[r.outcome] ?? 'neutral'} data-testid="upd-outcome">{knownOutcome(r.outcome) ? t[`updOutcome.${r.outcome}`] : t.unknownValue}</Badge></div>
            <p data-testid="upd-result-text">{knownOutcome(r.outcome) ? fmt(`updResult.${r.outcome}`, { to: r.to, from: r.from }) : t.updResultUnknown}</p>
            {#if r.outcome === 'rollback_failed'}<p class="secondary">{t.updRerunHint}</p>{/if}
            <KeyValueList items={[{ label: t.updResultFinishedAt, value: formatDateTime(r.finished_at) }]} />
            {#if r.reason_code}
              <details>
                <summary>{t.technicalDetails}</summary>
                <KeyValueList items={[{ label: t.updCode, value: r.reason_code, mono: true, testid: 'upd-reason' }]} />
              </details>
            {/if}
          </Card>
        {/if}

        {#if st.available}
          {@const a = st.available}
          <Card id="available" title={t.updAvailableHeading} icon="update" data-testid="upd-available">
            <div class="headline">
              <span class="num version" data-testid="upd-available-version">{a.version}</span>
              <Badge tone={a.channel === 'prerelease' ? 'information' : 'neutral'} data-testid="upd-channel">{t[`updChannel.${a.channel}`]}</Badge>
            </div>
            <KeyValueList
              items={[
                { label: t.updChannel, value: t[`updChannel.${a.channel}`] },
                { label: t.updDownloadSize, value: formatBytes(a.download_size), mono: true, testid: 'upd-size' },
                { label: t.updSignature, value: a.verified ? t.updSignatureChecked : t.updSignatureFailed, testid: 'upd-signature' },
              ]}
            />
            {#if a.verified}
              <p class="secondary" data-testid="upd-signature-note">{t.updSignatureCheckedNote}</p>
            {:else}
              <Notice tone="critical" data-testid="upd-unverified">
                <p>{a.verify_error ? updateErrorText(a.verify_error.code) : t.errorGeneric} {t.updSignatureFailedNote}</p>
              </Notice>
            {/if}
            <p><strong>{t.updRollbackLabel}:</strong> {t.updRollbackText}</p>

            <div class="notes">
              <h3 id="upd-notes-heading">{t.updNotesHeading}</h3>
              <p class="secondary" data-testid="upd-notes-source">{t.updNotesSource}</p>
              {#if a.notes.trim() === ''}
                <p class="secondary">{t.updNotesEmpty}</p>
              {:else}
                <!-- Release notes are untrusted: one text node, never markup (ADR 0010 U9). The box scrolls, so it
                     takes the focus to be scrolled with the keyboard (WCAG 2.1.1). -->
                <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
                <div class="notes-text" role="region" aria-labelledby="upd-notes-heading" tabindex="0" data-testid="upd-notes">{a.notes}</div>
              {/if}
            </div>

            {#if canApply && !confirmOpen}
              <div><button type="button" class="primary" bind:this={applyButton} onclick={() => void openConfirm()} data-testid="upd-apply">{t.updApply}</button></div>
            {/if}

            {#if canApply && confirmOpen}
              <!-- The confirmation (ADR 0010 U9): target, effect, permission and the way back before the action (spec 7.1). -->
              <section class="confirm" aria-labelledby="upd-confirm-heading" data-testid="upd-confirm">
                <h3 id="upd-confirm-heading">{t.updConfirmHeading}</h3>
                <KeyValueList
                  items={[
                    { label: t.updVersionLabel, value: fmt('updConfirmFromTo', { from: st.installed_version, to: a.version }) },
                    { label: t.updChannel, value: t[`updChannel.${a.channel}`] },
                    { label: t.updDownloadSize, value: formatBytes(a.download_size), mono: true },
                    { label: t.updSignature, value: t.updSignatureChecked },
                  ]}
                />
                <p>{t.updRollbackText}</p>
                {#key errorSeq}
                  {#if formError}<FormMessage tone="critical" testid="form-error">{formError.text}</FormMessage>{/if}
                {/key}
                <form class="form" novalidate onsubmit={(e) => void submitApply(e, a.version, false)}>
                  <FormField
                    id="upd-password"
                    type="password"
                    label={t.updPasswordLabel}
                    autocomplete="current-password"
                    help={t.updConfirmRestart}
                    bind:value={password}
                    bind:input={passwordInput}
                    error={formError?.field === 'password' ? formError.text : null}
                    disabled={busy}
                  />
                  <div class="buttons">
                    <button type="submit" class="primary" disabled={busy} data-testid="upd-confirm-submit">{busy ? t.updConfirmWorking : t.updApply}</button>
                    <button type="button" onclick={() => void closeConfirm()} disabled={busy}>{t.updCancel}</button>
                  </div>
                </form>
              </section>
            {/if}
          </Card>
        {/if}
      </div>
    </div>
  {/if}
</div>

<style>
  /* One column: the page is read from top to bottom, and lines stay short on wide windows. */
  .column {
    max-width: 48rem;
  }

  .secondary {
    color: var(--color-text-secondary);
  }

  .calm {
    display: flex;
    align-items: flex-start;
    gap: var(--space-2);
  }

  .calm :global(.icon) {
    flex: none;
    margin-top: 0.15em;
    color: var(--color-success-fg);
  }

  .actions,
  .buttons {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2) var(--space-3);
  }

  .headline {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2) var(--space-3);
  }

  .version {
    font-size: var(--text-lg);
    font-weight: var(--weight-semibold);
    overflow-wrap: anywhere;
  }

  .names {
    display: grid;
    gap: var(--space-1);
    padding-inline-start: var(--space-4);
    list-style: disc;
  }

  .notes {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: var(--space-1);
  }

  h3 {
    font-size: var(--text-md);
  }

  /* Plain text with its line breaks, in a box that scrolls instead of growing without end. */
  .notes-text {
    max-height: 20rem;
    overflow: auto;
    padding: var(--space-3);
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    font-size: var(--text-sm);
    background: var(--color-surface-muted);
    border: 1px solid var(--color-border);
    border-radius: var(--radius-md);
  }

  .confirm {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: var(--space-3);
    padding: var(--space-4);
    border: 1px solid var(--color-border-strong);
    border-radius: var(--radius-md);
  }

  .steps {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: var(--space-2);
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .steps li {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-1) var(--space-3);
    min-height: var(--target-min);
    color: var(--color-text-secondary);
  }

  .steps .mark {
    display: inline-flex;
    width: 1.25em;
    flex: none;
  }

  .steps li[data-status='done'] .mark {
    color: var(--color-success-fg);
  }

  .steps li[data-status='current'] {
    color: var(--color-text);
    font-weight: var(--weight-semibold);
  }

  .steps .state {
    font-size: var(--text-sm);
    font-weight: var(--weight-regular);
    color: var(--color-text-secondary);
  }

  @media (forced-colors: active) {
    .notes-text,
    .confirm {
      border-color: CanvasText;
    }
  }
</style>
