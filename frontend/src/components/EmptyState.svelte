<script lang="ts">
  import type { Snippet } from 'svelte';
  import type { HTMLAttributes } from 'svelte/elements';
  import type { IconName } from '../lib/icons';
  import type { Tone } from '../lib/ui';
  import Icon from './Icon.svelte';

  interface Props extends HTMLAttributes<HTMLDivElement> {
    icon?: IconName;
    tone?: Tone;
    title: string;
    description?: string;
    /** Small variant for inside a card: one line, no big icon. */
    compact?: boolean;
    /** Puts a live-region role on the title. Only for a state the user has to hear about (an error). */
    role?: 'alert' | 'status';
    titleTestid?: string;
    /** Actions below the text, for example a retry button. */
    children?: Snippet;
  }
  let { icon = 'info', tone = 'neutral', title, description, compact = false, role, titleTestid, children, ...rest }: Props = $props();
</script>

<div class="empty" class:compact data-tone={tone} {...rest}>
  <span class="mark"><Icon name={icon} /></span>
  <div class="text">
    <p class="title" {role} data-testid={titleTestid}>{title}</p>
    {#if description}<p class="description">{description}</p>{/if}
    {#if children}<div class="actions">{@render children()}</div>{/if}
  </div>
</div>

<style>
  .empty {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-7) var(--space-4);
    text-align: center;
    background: var(--color-surface);
    border: 1px dashed var(--color-border-strong);
    border-radius: var(--radius-lg);
    --tone-fg: var(--color-neutral-fg);
    --tone-bg: var(--color-neutral-bg);
    --tone-border: var(--color-neutral-border);
  }

  .empty[data-tone='warning'] {
    --tone-fg: var(--color-warning-fg);
    --tone-bg: var(--color-warning-bg);
    --tone-border: var(--color-warning-border);
  }

  .empty[data-tone='critical'] {
    --tone-fg: var(--color-critical-fg);
    --tone-bg: var(--color-critical-bg);
    --tone-border: var(--color-critical-border);
  }

  .empty[data-tone='success'] {
    --tone-fg: var(--color-success-fg);
    --tone-bg: var(--color-success-bg);
    --tone-border: var(--color-success-border);
  }

  .empty[data-tone='information'] {
    --tone-fg: var(--color-information-fg);
    --tone-bg: var(--color-information-bg);
    --tone-border: var(--color-information-border);
  }

  .mark {
    display: grid;
    place-items: center;
    width: 3rem;
    height: 3rem;
    font-size: 1.5rem;
    color: var(--tone-fg);
    background: var(--tone-bg);
    border: 1px solid var(--tone-border);
    border-radius: var(--radius-full);
  }

  .text {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: var(--space-1);
    max-width: 32rem;
    min-width: 0;
  }

  .title {
    font-weight: var(--weight-semibold);
    overflow-wrap: anywhere;
  }

  .description {
    color: var(--color-text-secondary);
  }

  .actions {
    margin-top: var(--space-2);
  }

  /* Inside a card: a quiet line, aligned left, without its own frame. */
  .compact {
    flex-direction: row;
    align-items: flex-start;
    gap: var(--space-2);
    padding: 0;
    text-align: start;
    background: none;
    border: 0;
  }

  .compact .mark {
    width: auto;
    height: auto;
    font-size: 1.25rem;
    color: var(--color-text-secondary);
    background: none;
    border: 0;
  }

  .compact .title {
    font-weight: var(--weight-regular);
    color: var(--color-text-secondary);
  }
</style>
