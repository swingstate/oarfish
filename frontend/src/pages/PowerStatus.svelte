<script lang="ts">
  import { onMount } from 'svelte';
  import { t } from '../lib/i18n';
  import { probeHealth, type PowerAction } from '../lib/power';

  // ADR 0011 B6; docs/UI.md, "Restart and shutdown". After an accepted request
  // App.svelte shows this view instead of the shell: no navigation, no other
  // page, no other request. It asks GET /healthz (public, no session) every
  // 5 s and says what is expected, not what happened: a polling browser cannot
  // tell a shutdown from a lost network. The polling stops when the device is
  // back (restart), at the first failed answer (shutdown: nothing more to
  // learn), when the sign-in link is followed, and in the component's cleanup.
  interface Props {
    action: PowerAction;
  }
  let { action }: Props = $props();

  const pollMs = 5000;
  const lateMs = 5 * 60 * 1000;

  // waiting: no failed answer yet. down: the device stopped answering. back:
  // it answered again after that (restart). late: no answer for 5 minutes
  // (restart). off: it stopped answering (shutdown).
  let phase = $state<'waiting' | 'down' | 'back' | 'late' | 'off'>('waiting');
  let heading: HTMLElement | undefined = $state();
  let alive = true;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let failed = false;
  let lastAnswer = Date.now();

  function stop(): void {
    alive = false;
    clearTimeout(timer);
  }

  function schedule(): void {
    clearTimeout(timer);
    if (alive) timer = setTimeout(() => void poll(), pollMs);
  }

  async function poll(): Promise<void> {
    const ok = await probeHealth();
    if (!alive) return;
    if (ok) {
      lastAnswer = Date.now();
      if (failed && action === 'reboot') {
        phase = 'back';
        stop();
        return;
      }
    } else {
      failed = true;
      if (action === 'poweroff') {
        phase = 'off';
        stop();
        return;
      }
      phase = Date.now() - lastAnswer >= lateMs ? 'late' : 'down';
    }
    schedule();
  }

  onMount(() => {
    heading?.focus();
    schedule();
    return stop;
  });

  const text = $derived(
    action === 'poweroff'
      ? phase === 'off'
        ? t.pwrAfterPoweroff
        : t.pwrDuringPoweroff
      : phase === 'back'
        ? t.pwrAfterReboot
        : phase === 'late'
          ? t.pwrRebootLate
          : t.pwrDuringReboot,
  );
  // The sign-in is the start page of the app: a new load of it ends this view.
  const signInHref = location.pathname + location.search;
</script>

<div class="status" data-testid="pwr-status" data-phase={phase}>
  <h1 tabindex="-1" bind:this={heading}>{action === 'poweroff' ? t.pwrStatusPoweroff : t.pwrStatusReboot}</h1>
  <p role="status" aria-live="polite" data-testid="pwr-status-text">{text}</p>
  {#if phase === 'back'}
    <p><a href={signInHref} onclick={stop} data-testid="pwr-sign-in">{t.pwrSignIn}</a></p>
  {/if}
  <p data-testid="pwr-status-tunnel">{phase === 'back' ? t.pwrTunnelBack : t.pwrTunnel}</p>
</div>

<style>
  .status {
    display: grid;
    gap: var(--space-3);
  }

  h1 {
    font-size: var(--text-xl);
  }
</style>
