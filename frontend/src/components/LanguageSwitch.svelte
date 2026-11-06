<script lang="ts">
  import { t } from '../lib/i18n';
  import { lang, setLang, type Lang } from '../lib/lang.svelte';

  // Each language is named in itself, so it can be found in either language.
  const choices: { code: Lang; label: () => string }[] = [
    { code: 'de', label: () => t.langGerman },
    { code: 'en', label: () => t.langEnglish },
  ];
</script>

<!-- Two native buttons: Tab and Enter or Space work, the pressed one is announced and
     is also marked by a bar, not by colour alone. -->
<div class="switch" role="group" aria-label={t.langLabel}>
  {#each choices as choice (choice.code)}
    <button type="button" lang={choice.code} aria-pressed={lang() === choice.code} onclick={() => setLang(choice.code)}>
      {choice.label()}
    </button>
  {/each}
</div>

<style>
  .switch {
    display: flex;
    gap: var(--space-1);
  }

  button {
    flex: 1;
    min-height: var(--target-min);
    padding: var(--space-1) var(--space-2);
    font-size: var(--text-sm);
  }

  button[aria-pressed='true'] {
    color: var(--color-accent-subtle-fg);
    background: var(--color-accent-subtle-bg);
    font-weight: var(--weight-semibold);
    box-shadow: inset 0 -3px 0 var(--color-accent);
  }

  @media (forced-colors: active) {
    button[aria-pressed='true'] {
      border: 2px solid Highlight;
    }
  }
</style>
