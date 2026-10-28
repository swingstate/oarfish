<script lang="ts">
  import type { Snippet } from 'svelte';
  import type { HTMLAttributes } from 'svelte/elements';
  import type { IconName } from '../lib/icons';
  import type { Tone } from '../lib/ui';
  import Icon from './Icon.svelte';

  interface Props extends HTMLAttributes<HTMLSpanElement> {
    tone?: Tone;
    /** Optional shape next to the text. The text still says it all. */
    icon?: IconName;
    children: Snippet;
  }
  let { tone = 'neutral', icon, children, ...rest }: Props = $props();
</script>

<span class="badge" data-tone={tone} {...rest}>{#if icon}<Icon name={icon} />{/if}{@render children()}</span>

<style>
  .badge {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    max-width: 100%;
    min-height: 1.375rem;
    padding: 0 var(--space-2);
    font-size: var(--text-xs);
    font-weight: var(--weight-medium);
    line-height: var(--leading-snug);
    color: var(--tone-fg);
    background: var(--tone-bg);
    border: 1px solid var(--tone-border);
    border-radius: var(--radius-sm);
    --tone-bg: var(--color-neutral-bg);
    --tone-fg: var(--color-neutral-fg);
    --tone-border: var(--color-neutral-border);
  }

  .badge :global(.icon) {
    width: 1em;
    height: 1em;
  }

  .badge[data-tone='success'] {
    --tone-bg: var(--color-success-bg);
    --tone-fg: var(--color-success-fg);
    --tone-border: var(--color-success-border);
  }

  .badge[data-tone='information'] {
    --tone-bg: var(--color-information-bg);
    --tone-fg: var(--color-information-fg);
    --tone-border: var(--color-information-border);
  }

  .badge[data-tone='warning'] {
    --tone-bg: var(--color-warning-bg);
    --tone-fg: var(--color-warning-fg);
    --tone-border: var(--color-warning-border);
  }

  .badge[data-tone='critical'] {
    --tone-bg: var(--color-critical-bg);
    --tone-fg: var(--color-critical-fg);
    --tone-border: var(--color-critical-border);
  }
</style>
