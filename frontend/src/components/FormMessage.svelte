<script lang="ts">
  import type { Snippet } from 'svelte';
  import { toneIcon, type Tone } from '../lib/ui';
  import Icon from './Icon.svelte';

  interface Props {
    tone: Tone;
    children: Snippet;
    testid?: string;
  }
  let { tone, children, testid }: Props = $props();
</script>

<!-- The live region of a form: an error is announced at once (alert), a
     confirmation or a state politely (status). Only forms have one; Home keeps
     the alert list as its only live region. -->
<div class="message" data-tone={tone} role={tone === 'critical' ? 'alert' : 'status'} data-testid={testid}>
  <Icon name={toneIcon[tone]} />
  <div class="body">{@render children()}</div>
</div>

<style>
  .message {
    display: flex;
    align-items: flex-start;
    gap: var(--space-3);
    padding: var(--space-3) var(--space-4);
    color: var(--tone-fg);
    background: var(--tone-bg);
    border: 1px solid var(--tone-border);
    border-radius: var(--radius-md);
    --tone-bg: var(--color-neutral-bg);
    --tone-fg: var(--color-neutral-fg);
    --tone-border: var(--color-neutral-border);
  }

  .message :global(.icon) {
    margin-top: 0.1em;
  }

  .body {
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .message[data-tone='success'] {
    --tone-bg: var(--color-success-bg);
    --tone-fg: var(--color-success-fg);
    --tone-border: var(--color-success-border);
  }

  .message[data-tone='information'] {
    --tone-bg: var(--color-information-bg);
    --tone-fg: var(--color-information-fg);
    --tone-border: var(--color-information-border);
  }

  .message[data-tone='warning'] {
    --tone-bg: var(--color-warning-bg);
    --tone-fg: var(--color-warning-fg);
    --tone-border: var(--color-warning-border);
  }

  .message[data-tone='critical'] {
    --tone-bg: var(--color-critical-bg);
    --tone-fg: var(--color-critical-fg);
    --tone-border: var(--color-critical-border);
  }
</style>
