<script>
  export let items = [];

  const fmtMoney = (value) => `$${Number(value || 0).toFixed(2)}`;
  const fmtPct = (value) => `${(Number(value || 0) * 100).toFixed(1)}%`;
  const tone = (item) => {
    if (item.grade === 'A' || item.grade === 'B') return 'strong';
    if (item.spread_ratio > 0.35) return 'risk';
    if (item.momentum_7d > 0) return 'watch';
    return 'neutral';
  };
  const span = (item) => Math.max(1, Math.min(4, Math.round(Number(item.weight || 1) / 35)));
</script>

<div class="heatmap">
  {#each items as item}
    <article class={`tile ${tone(item)}`} style={`grid-column: span ${span(item)}; grid-row: span ${span(item)};`}>
      <div class="tile-head">
        <b>{item.name}</b>
        {#if item.grade}<span>G{item.grade}</span>{/if}
      </div>
      <strong>{fmtMoney(item.price)}</strong>
      <div class="tile-metrics">
        <span>liq {fmtPct(item.liquidity_score)}</span>
        <span>spr {fmtPct(item.spread_ratio)}</span>
      </div>
    </article>
  {/each}
</div>
