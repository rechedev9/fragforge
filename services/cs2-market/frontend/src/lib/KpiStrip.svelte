<script>
  export let items = [];
</script>

<section class="kpis" aria-label="Market KPIs">
  {#each items as item}
    <article class="kpi">
      <div class="kpi-line"></div>
      <b>{item.value}</b>
      <span>{item.label}</span>
      <small>{item.detail}</small>
    </article>
  {/each}
</section>
