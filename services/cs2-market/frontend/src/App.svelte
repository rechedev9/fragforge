<script>
  import Heatmap from './lib/Heatmap.svelte';
  import KpiStrip from './lib/KpiStrip.svelte';
  import RecommendationCard from './lib/RecommendationCard.svelte';
  import RiskPanel from './lib/RiskPanel.svelte';

  let data = null;
  let status = 'loading';
  let query = '';
  let action = 'all';
  let grade = 'all';

  $: recommendations = data?.recommendations ?? [];
  $: filteredRecommendations = recommendations.filter((item) => {
    const matchesQuery = !query || item.market_hash_name.toLowerCase().includes(query.toLowerCase());
    const matchesAction = action === 'all' || item.action === action;
    const matchesGrade = grade === 'all' || item.grade === grade;
    return matchesQuery && matchesAction && matchesGrade;
  });

  async function loadData() {
    try {
      const embedded = document.getElementById('dashboard-data');
      if (embedded?.textContent) {
        data = JSON.parse(embedded.textContent);
        status = 'ready';
        return;
      }
      const response = await fetch('./dashboard-data.json', { cache: 'no-store' });
      if (!response.ok) {
        throw new Error(`dashboard-data.json returned ${response.status}`);
      }
      data = await response.json();
      status = 'ready';
    } catch (error) {
      status = error instanceof Error ? error.message : 'failed to load dashboard data';
    }
  }

  loadData();
</script>

{#if status === 'loading'}
  <main class="loading">Loading CS2 market dashboard...</main>
{:else if status !== 'ready'}
  <main class="loading error">{status}</main>
{:else}
  <header class="hero">
    <img class="hero-art" src="./assets/market-hero.png" alt="" aria-hidden="true" />
    <div class="hero-shade"></div>
    <div class="hero-content">
      <nav class="desk-nav" aria-label="Dashboard sections">
        <span>Market Desk</span>
        <span>Risk</span>
        <span>Signals</span>
        <span>Heatmap</span>
      </nav>
      <div class="hero-copy">
        <p class="eyebrow">CS2 skins investment research</p>
        <h1>{data.title}</h1>
        <p class="subtitle">
          Institutional-style watchlist for skins: thesis, invalidation, allocation caps, liquidity and model evidence.
        </p>
      </div>
      <div class="hero-footer">
        <div class="snapshot">
          <span>Snapshot</span>
          <b>{data.summary.latest_as_of || 'n/a'}</b>
        </div>
        <div class="disclaimer">
          Research only. No guaranteed profit. Refresh market data before publishing or buying.
        </div>
      </div>
    </div>
  </header>

  <main class="shell">
    <KpiStrip items={data.kpis} />

    <section class="briefing">
      <div>
        <span class="label">Desk stance</span>
        <strong>{data.risk_gate.status}</strong>
        <p>{data.risk_gate.message}</p>
      </div>
      <div>
        <span class="label">Execution policy</span>
        <strong>Strict invalidation</strong>
        <p>Entries are capped by reference price, spread, visible quantity and backtest evidence.</p>
      </div>
      <div>
        <span class="label">Current universe</span>
        <strong>{data.summary.skin_rows} skin rows</strong>
        <p>{data.summary.signals} active recommendations, {data.summary.predictions} ML watchlist rows.</p>
      </div>
    </section>

    <section class="workspace">
      <div class="recommendations-panel">
        <div class="section-head">
          <div>
            <h2>Investment Committee Recommendations</h2>
            <p>Conviction, thesis, invalidation, sizing, liquidity and model evidence in one place.</p>
          </div>
          <div class="filters">
            <input bind:value={query} type="search" placeholder="Search skin" aria-label="Search skin" />
            <select bind:value={action} aria-label="Filter by action">
              <option value="all">All actions</option>
              <option value="buy">Buy</option>
              <option value="watch">Watch</option>
              <option value="avoid">Avoid</option>
              <option value="sell">Sell</option>
            </select>
            <select bind:value={grade} aria-label="Filter by grade">
              <option value="all">All grades</option>
              <option value="A">Grade A</option>
              <option value="B">Grade B</option>
              <option value="C">Grade C</option>
              <option value="D">Grade D</option>
            </select>
          </div>
        </div>

        <div class="cards">
          {#each filteredRecommendations as rec}
            <RecommendationCard recommendation={rec} />
          {:else}
            <div class="empty">No recommendations match the current filters.</div>
          {/each}
        </div>
      </div>

      <aside class="side">
        <RiskPanel gate={data.risk_gate} summary={data.summary} sources={data.sources} />
      </aside>
    </section>

    <section class="market-map">
      <div class="section-head compact">
        <div>
          <h2>Market Heatmap</h2>
          <p>Treemap-style view of liquidity, score and spread across the skin universe.</p>
        </div>
      </div>
      <Heatmap items={data.heatmap} />
    </section>
  </main>
{/if}
