<script lang="ts">
  import { iconPaths, type IconName } from '../lib/icons';

  let { name }: { name: IconName } = $props();
</script>

<!-- Decorative: the text next to it carries the meaning. -->
<svg class="icon" viewBox="0 0 24 24" aria-hidden="true" focusable="false">
  {#each iconPaths[name] as d (d)}<path {d} />{/each}
</svg>

<style>
  .icon {
    flex: none;
    width: 1.25em;
    height: 1.25em;
    fill: none;
    stroke: currentColor;
    stroke-width: 1.75;
    stroke-linecap: round;
    stroke-linejoin: round;
  }
</style>
