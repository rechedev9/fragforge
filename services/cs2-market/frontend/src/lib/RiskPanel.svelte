<script>
  export let gate;
  export let summary;
  export let sources;

  const pct = (value) => `${(Number(value || 0) * 100).toFixed(1)}%`;
</script>

<section class="risk-panel">
  <div class="panel-title">
    <h2>Risk Gate</h2>
    <span>Policy</span>
  </div>
  <div class="status">
    <span>{gate.status}</span>
    <b>{pct(summary.backtest_return)}</b>
    <small>Net return@K</small>
  </div>
  <p class="message">{gate.message}</p>

  <div class="rules">
    {#each gate.rules as rule}
      <div>
        <b>{rule.label}</b>
        <span>{rule.text}</span>
      </div>
    {/each}
  </div>

  <h3>Sources</h3>
  <dl>
    <dt>Signals</dt><dd>{sources.signals}</dd>
    <dt>Features</dt><dd>{sources.features}</dd>
    <dt>Predictions</dt><dd>{sources.predictions}</dd>
  </dl>
</section>
