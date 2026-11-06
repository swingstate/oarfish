<script lang="ts">
  // No props: the mark is always the same.
</script>

<!-- The Oarfish mark (assets/oarfish-mark-mono.svg, the same paths), single colour: it takes the text
     colour of its parent, which sets --color-logo-ink. Decorative: the brand name next to it is the text. -->
<svg class="mark" viewBox="0 0 64 64" aria-hidden="true" focusable="false">
  <path d="M12 38V29M20 38V25M28 38V20M36 38V15M44 38V10M52 35V19" fill="none" stroke="currentColor" stroke-width="4" stroke-linecap="round" />
  <path fill="currentColor" fill-rule="evenodd" d="M9 35H55A5 5 0 0 1 55 45H9A5 5 0 0 1 9 35ZM50.8 40a2.2 2.2 0 1 0 4.4 0a2.2 2.2 0 1 0-4.4 0Z" />
  <path d="M46 45C46 52 40 54 28 54" fill="none" stroke="currentColor" stroke-width="4" stroke-linecap="round" />
</svg>

<style>
  .mark {
    flex: none;
    width: 1.75rem;
    height: 1.75rem;
    color: var(--color-logo-ink);
  }
</style>
