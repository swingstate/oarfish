<script lang="ts">
  import type { HTMLInputAttributes } from 'svelte/elements';

  interface Props {
    /** Unique within the page; the label, help and error are tied to it. */
    id: string;
    label: string;
    value: string;
    type?: 'text' | 'password';
    autocomplete: HTMLInputAttributes['autocomplete'];
    help?: string;
    /** A specific error for this field; it marks the field invalid. */
    error?: string | null;
    disabled?: boolean;
    input?: HTMLInputElement;
  }
  let { id, label, value = $bindable(), type = 'text', autocomplete, help, error = null, disabled = false, input = $bindable() }: Props = $props();

  const describedBy = $derived([help ? `${id}-help` : null, error ? `${id}-error` : null].filter(Boolean).join(' ') || undefined);
</script>

<div class="field">
  <label for={id}>{label}</label>
  {#if help}<p class="help" id="{id}-help">{help}</p>{/if}
  <input
    {id}
    {type}
    name={id}
    bind:value
    bind:this={input}
    {autocomplete}
    {disabled}
    autocapitalize="off"
    spellcheck="false"
    required
    aria-invalid={error ? 'true' : undefined}
    aria-describedby={describedBy}
  />
  {#if error}<p class="error" id="{id}-error">{error}</p>{/if}
</div>

<style>
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
    color: var(--color-text-secondary);
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
  }

  .error {
    font-size: var(--text-sm);
    color: var(--color-critical-fg);
    font-weight: var(--weight-medium);
  }

  @media (forced-colors: active) {
    input {
      border-color: CanvasText;
    }
  }
</style>
