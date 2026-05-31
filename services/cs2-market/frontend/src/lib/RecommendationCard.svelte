<script>
  export let recommendation;

  const fmtMoney = (value, currency) => `${currency || 'USD'} ${Number(value || 0).toFixed(2)}`;
  const fmtPct = (value) => `${(Number(value || 0) * 100).toFixed(1)}%`;

  $: actionClass = recommendation.action === 'buy' ? 'buy' : recommendation.action === 'watch' ? 'watch' : 'avoid';
</script>

<article class="recommendation">
  <div class="grade-rail" data-grade={recommendation.grade}></div>
  <div class="skin">
    {#if recommendation.image}
      <img src={recommendation.image} alt={recommendation.market_hash_name} />
    {:else}
      <span>No image</span>
    {/if}
  </div>
  <div class="content">
    <div class="topline">
      <div>
        <strong>{recommendation.decision}</strong>
        <h3>{recommendation.market_hash_name}</h3>
        <small>{recommendation.suggested_allocation}</small>
      </div>
      <div class="badges">
        <span class={`badge ${actionClass}`}>{recommendation.action.toUpperCase()}</span>
        <span class="badge grade">Grade {recommendation.grade}</span>
      </div>
    </div>

    <div class="metrics">
      <div><b>{fmtMoney(recommendation.price, recommendation.currency)}</b><span>Reference price</span></div>
      <div><b>{Number(recommendation.score || 0).toFixed(1)}</b><span>Signal score</span></div>
      <div><b>{fmtPct(recommendation.confidence)}</b><span>Confidence</span></div>
      <div><b>{recommendation.risk_level}</b><span>Risk</span></div>
    </div>

    <p><b>Thesis:</b> {recommendation.thesis}</p>
    <p><b>Invalidation:</b> {recommendation.invalidation}</p>

    <div class="evidence">
      {#each recommendation.evidence as item}
        <span>{item}</span>
      {/each}
    </div>
  </div>
</article>
