<script lang="ts">
  import { fetchReadyState, type ReadyState } from './lib/health';

  let state = $state<ReadyState>('checking');

  $effect(() => {
    void fetchReadyState().then((s) => {
      state = s;
    });
  });
</script>

<main>
  <h1>Platform</h1>
  <p>API: <strong data-testid="ready-state">{state}</strong></p>
</main>

<style>
  main {
    font-family: system-ui, sans-serif;
    max-width: 40rem;
    margin: 4rem auto;
    padding: 0 1rem;
  }
</style>
