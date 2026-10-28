<script lang="ts">
  import type { Snippet } from 'svelte';
  import type { HTMLAttributes } from 'svelte/elements';
  import { toneIcon, type Tone } from '../lib/ui';
  import Icon from './Icon.svelte';

  interface Props extends HTMLAttributes<HTMLDivElement> {
    tone?: Tone;
    children: Snippet;
  }
  let { tone = 'neutral', children, ...rest }: Props = $props();
</script>

<!-- Not a live region: a notice is part of the page, announced only by reading it. -->
<div class="notice" data-tone={tone} {...rest}>
  <Icon name={toneIcon[tone]} />
  <div class="body">{@render children()}</div>
</div>

<style>
  .notice {
    display: flex;
    align-items: flex-start;
    gap: var(--space-3);
    padding: var(--space-3) var(--space-4);
    color: var(--tone-fg);
    background: var(--tone-bg);
    border: 1px solid var(--tone-border);
    border-radius: var(--radius-md);
    font-size: var(--text-md);
    --tone-bg: var(--color-neutral-bg);
    --tone-fg: var(--color-neutral-fg);
    --tone-border: var(--color-neutral-border);
  }

  .notice :global(.icon) {
    margin-top: 0.1em;
  }

  .body {
    min-width: 0;
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: var(--space-1);
    overflow-wrap: anywhere;
  }

  .notice[data-tone='success'] {
    --tone-bg: var(--color-success-bg);
    --tone-fg: var(--color-success-fg);
    --tone-border: var(--color-success-border);
  }

  .notice[data-tone='information'] {
    --tone-bg: var(--color-information-bg);
    --tone-fg: var(--color-information-fg);
    --tone-border: var(--color-information-border);
  }

  .notice[data-tone='warning'] {
    --tone-bg: var(--color-warning-bg);
    --tone-fg: var(--color-warning-fg);
    --tone-border: var(--color-warning-border);
  }

  .notice[data-tone='critical'] {
    --tone-bg: var(--color-critical-bg);
    --tone-fg: var(--color-critical-fg);
    --tone-border: var(--color-critical-border);
  }
</style>
