# Design Baseline: Principiis obsta (kubenow)

**Principiis obsta** — resist the beginnings.

kubenow is designed to surface systemic failure **while it is still cheap to fix**.

It does not optimize exploration, trendlines, or historical dashboards.
It shows only what is broken **right now** — before cascading failure begins.

---

## The Principle

Applied to cluster operations:

**Intervene where trajectories form, not where outcomes are already fixed.**

Traditional monitoring asks: "What happened over time?"

kubenow asks: "What is broken right now that will become expensive if ignored?"

If something is failing, **surface it immediately**.
If everything is healthy, **stay silent**.

---

## Design Priorities

kubenow prioritizes:
- **Active failures** over historical trends
- **Structural problems** over transient noise
- **Immediate intervention** over root cause analysis
- **Silence as success** over continuous output

Non-goals:
- Dashboards showing healthy resources
- Historical graphs for exploration
- Alerts for predicted failures
- Metrics for metrics' sake

---

## Why This Matters

Most Kubernetes tools optimize for:
- Visibility (show me everything)
- Exploration (what might be wrong?)
- Long-term trends (what happened last week?)

kubenow optimizes for:
- Intervention (what do I fix right now?)
- Clarity (no exploration needed)
- Attention preservation (silence when healthy)

**You don't need a dashboard to know nothing is broken.**

---

## Related Projects

This principle is applied across different surfaces:

- **[Chainwatch](https://github.com/ppiankov/chainwatch)** - Execution chain control for AI agents
- **[kubenow](https://github.com/ppiankov/kubenow)** - Cluster health intervention (not exploration)
- **[infranow](https://github.com/ppiankov/infranow)** - Metric-driven triage (silence as success)

Same principle. Different surfaces.

---

**If a system needs a dashboard to justify its existence, it is already too late.**

---

