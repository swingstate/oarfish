<script lang="ts">
  // No props: the mark is always the same.
</script>

<!-- The Oarfish mark: decorative, the brand name next to it is the text. -->
<svg class="mark" viewBox="0 0 24 24" aria-hidden="true" focusable="false">
  <path d="M2.5 12c3-5.5 9.5-6.5 13.5-3.2L21.5 6v12L16 15.2c-4 3.3-10.5 2.3-13.5-3.2z" />
  <circle cx="8" cy="11" r="1" />
</svg>

<style>
  .mark {
    flex: none;
    width: 1.5rem;
    height: 1.5rem;
    fill: var(--color-accent);
  }

  .mark circle {
    fill: var(--color-surface);
  }
</style>
