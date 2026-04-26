// Package metrics binds the GembaEvent stream to a Prometheus
// /metrics endpoint so operators can scrape adaptor health, session
// activity, escalation pressure, and budget posture without standing
// up an OTEL collector. gm-e3.6.2.
//
// Wiring:
//
//   - cmd/gemba serve constructs a Collector via NewCollector and
//     registers it on a per-process prometheus.Registry.
//   - The same boot path calls Collector.Run(ctx, hub) to subscribe
//     the collector to the events.Hub. Every Publish drives the
//     relevant metric.
//   - internal/server/router.go mounts Collector.Handler() at GET
//     /metrics. The endpoint sits behind the same auth middleware as
//     /api/*: a deployment with auth enabled requires its scrape job
//     to present credentials. A loopback-only deployment leaves auth
//     off and Prometheus scrapes anonymously, matching the standard
//     convention for /metrics behind a reverse proxy.
//
// Payload conventions:
//
// GembaEvent.Payload is a free-form map (per gm-hjx). The collector
// reads opportunistically — if a key isn't present, the metric is
// just not observed for that event. This keeps adaptors free to
// evolve their payloads without breaking metrics.
//
//   - workitem.* events:    payload["state_category"] (string) names
//     the new state for state_duration_seconds.
//   - session.cost_sample:  payload["cost_dollars"] (float64) names
//     the dollar cost to add to the counter.
//   - escalation.opened:    payload["source_kind"] (string) groups
//     the open_count gauge.
//   - workspace.acquired:   payload["acquire_duration_seconds"]
//     (float64) feeds the latency histogram.
//
// Cardinality:
//
// The assignment label on assignment_total_cost_dollars is a known
// high-cardinality risk — one series per active assignment. Acceptable
// at expected scale (≤ a few thousand simultaneous assignments per
// process); document the trade-off when introducing the metric to a
// larger deployment.
package metrics
