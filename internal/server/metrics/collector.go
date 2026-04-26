package metrics

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/MikeBengtson/gemba/internal/events"
)

// Default histogram buckets covering "fast UI" up to "long-running
// async session" durations. Operators with different workloads can
// override via NewCollectorWithBuckets.
var (
	defaultStateDurationBuckets = prometheus.ExponentialBuckets(
		1, // 1s
		4, // ×4 each bucket
		8, // → 1s, 4s, 16s, 64s, 256s (~4m), 1024s (~17m), 4096s (~68m), 16384s (~4.5h)
	)
	defaultAcquireLatencyBuckets = prometheus.ExponentialBuckets(
		0.01, // 10ms
		3,    // ×3 each bucket
		8,    // → 10ms, 30ms, 90ms, 270ms, 810ms, 2.4s, 7.3s, 22s
	)
)

// trackedWorkItemsCap caps how many workitems the collector remembers
// in-flight at once. Bounds memory growth in pathological cases
// (adaptors that emit created without ever emitting closed, or hub
// drops). On overflow the oldest entry is evicted.
const trackedWorkItemsCap = 10_000

// Collector owns the prometheus metric instances and the in-flight
// state needed to translate GembaEvents into metric observations.
// One Collector per gemba serve process.
type Collector struct {
	reg *prometheus.Registry

	workitemStateDuration *prometheus.HistogramVec
	assignmentCost        *prometheus.CounterVec
	escalationOpenCount   *prometheus.GaugeVec
	sessionSpawnTotal     prometheus.Counter
	workspaceAcquireSecs  prometheus.Histogram

	mu              sync.Mutex
	workItemEntries map[string]workItemEntry       // workitem id → enteredAt + state
	workItemOrder   []string                       // FIFO of ids for bounded eviction
	openEscalations map[string]map[string]struct{} // source_kind → set of open ids
}

type workItemEntry struct {
	adaptorID string
	state     string
	enteredAt time.Time
}

// NewCollector builds a Collector with the default histogram buckets
// and registers all metrics on a fresh prometheus.Registry. Use
// Handler() to mount the /metrics endpoint and Run(ctx, hub) to
// start consuming events. The returned Collector is safe for
// concurrent use.
func NewCollector() *Collector {
	return NewCollectorWithBuckets(defaultStateDurationBuckets, defaultAcquireLatencyBuckets)
}

// NewCollectorWithBuckets is NewCollector with caller-controlled
// histogram buckets. Used by tests that want tight buckets and by
// operators with workload profiles that don't fit the defaults.
func NewCollectorWithBuckets(stateBuckets, acquireBuckets []float64) *Collector {
	reg := prometheus.NewRegistry()
	c := &Collector{
		reg: reg,
		workitemStateDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: "gemba",
				Subsystem: "workitem",
				Name:      "state_duration_seconds",
				Help:      "Time a work item spent in a given state before transitioning out, by adaptor and state_category.",
				Buckets:   stateBuckets,
			},
			[]string{"adaptor", "state_category"},
		),
		assignmentCost: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "gemba",
				Subsystem: "assignment",
				Name:      "total_cost_dollars",
				Help:      "Cumulative dollar cost reported via session.cost_sample, by adaptor and assignment.",
			},
			[]string{"adaptor", "assignment"},
		),
		escalationOpenCount: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "gemba",
				Subsystem: "escalation",
				Name:      "open_count",
				Help:      "Currently-open escalations grouped by source_kind. Decrements on escalation.resolved.",
			},
			[]string{"source_kind"},
		),
		sessionSpawnTotal: prometheus.NewCounter(
			prometheus.CounterOpts{
				Namespace: "gemba",
				Subsystem: "session",
				Name:      "spawn_total",
				Help:      "Sessions whose workspace was acquired (proxy for spawn). Rate gives session_spawn_rate.",
			},
		),
		workspaceAcquireSecs: prometheus.NewHistogram(
			prometheus.HistogramOpts{
				Namespace: "gemba",
				Subsystem: "workspace",
				Name:      "acquire_latency_seconds",
				Help:      "Time taken to acquire a workspace, observed when workspace.acquired carries acquire_duration_seconds.",
				Buckets:   acquireBuckets,
			},
		),
		workItemEntries: make(map[string]workItemEntry, trackedWorkItemsCap),
		openEscalations: make(map[string]map[string]struct{}),
	}
	reg.MustRegister(
		c.workitemStateDuration,
		c.assignmentCost,
		c.escalationOpenCount,
		c.sessionSpawnTotal,
		c.workspaceAcquireSecs,
	)
	return c
}

// Handler returns the http.Handler for GET /metrics.
func (c *Collector) Handler() http.Handler {
	return promhttp.HandlerFor(c.reg, promhttp.HandlerOpts{
		Registry: c.reg,
	})
}

// Registry returns the underlying registry. Tests use it to scrape
// directly without going through the HTTP layer.
func (c *Collector) Registry() *prometheus.Registry { return c.reg }

// Run subscribes to the events hub with no filter and feeds every
// matching event through observe(). Returns when ctx is cancelled or
// the hub closes. Safe to call once per Collector.
func (c *Collector) Run(ctx context.Context, hub *events.Hub) {
	if hub == nil {
		return
	}
	ch := hub.Subscribe(ctx, events.Filter{})
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			c.observe(ev)
		}
	}
}

// observe is the fan-in point — exported only via Run, but pulled
// out so tests can drive metric updates synchronously without
// spinning up a hub.
func (c *Collector) observe(ev events.GembaEvent) {
	switch ev.Kind {
	case events.WorkItemCreated:
		c.onWorkItemTransition(ev, true)
	case events.WorkItemUpdated:
		c.onWorkItemTransition(ev, false)
	case events.WorkItemClosed:
		c.onWorkItemClosed(ev)
	case events.SessionCostSample:
		c.onCostSample(ev)
	case events.EscalationOpened:
		c.onEscalationOpened(ev)
	case events.EscalationResolved:
		c.onEscalationResolved(ev)
	case events.WorkspaceAcquired:
		c.onWorkspaceAcquired(ev)
	}
}

// onWorkItemTransition records the duration the workitem spent in
// its previous state and arms a new entry for its current state.
// onCreate true skips the duration observation (no prior state).
func (c *Collector) onWorkItemTransition(ev events.GembaEvent, onCreate bool) {
	id := ev.WorkItemID
	if id == "" {
		return
	}
	state := stringFromPayload(ev.Payload, "state_category")

	c.mu.Lock()
	defer c.mu.Unlock()

	if !onCreate {
		if prev, ok := c.workItemEntries[id]; ok && prev.state != "" && prev.state != state {
			c.workitemStateDuration.
				WithLabelValues(prev.adaptorID, prev.state).
				Observe(time.Since(prev.enteredAt).Seconds())
		}
	}

	if state == "" {
		// No state info — don't arm a tracker we'd never observe later.
		return
	}
	if _, existed := c.workItemEntries[id]; !existed {
		c.workItemOrder = append(c.workItemOrder, id)
		c.evictIfFull()
	}
	c.workItemEntries[id] = workItemEntry{
		adaptorID: ev.Source.AdaptorID,
		state:     state,
		enteredAt: ev.At,
	}
}

func (c *Collector) onWorkItemClosed(ev events.GembaEvent) {
	id := ev.WorkItemID
	if id == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if prev, ok := c.workItemEntries[id]; ok {
		c.workitemStateDuration.
			WithLabelValues(prev.adaptorID, prev.state).
			Observe(time.Since(prev.enteredAt).Seconds())
		delete(c.workItemEntries, id)
		// Lazy: leave id in workItemOrder; eviction handles dead refs.
	}
}

func (c *Collector) onCostSample(ev events.GembaEvent) {
	cost := floatFromPayload(ev.Payload, "cost_dollars")
	if cost <= 0 {
		return
	}
	assignment := ev.AssignmentID
	if assignment == "" {
		assignment = "unknown"
	}
	c.assignmentCost.WithLabelValues(ev.Source.AdaptorID, assignment).Add(cost)
}

func (c *Collector) onEscalationOpened(ev events.GembaEvent) {
	if ev.ID == "" {
		return
	}
	kind := stringFromPayload(ev.Payload, "source_kind")
	if kind == "" {
		kind = "unknown"
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	set, ok := c.openEscalations[kind]
	if !ok {
		set = make(map[string]struct{})
		c.openEscalations[kind] = set
	}
	set[ev.ID] = struct{}{}
	c.escalationOpenCount.WithLabelValues(kind).Set(float64(len(set)))
}

func (c *Collector) onEscalationResolved(ev events.GembaEvent) {
	if ev.ID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for kind, set := range c.openEscalations {
		if _, ok := set[ev.ID]; ok {
			delete(set, ev.ID)
			c.escalationOpenCount.WithLabelValues(kind).Set(float64(len(set)))
			return
		}
	}
}

func (c *Collector) onWorkspaceAcquired(ev events.GembaEvent) {
	c.sessionSpawnTotal.Inc()
	if dur := floatFromPayload(ev.Payload, "acquire_duration_seconds"); dur > 0 {
		c.workspaceAcquireSecs.Observe(dur)
	}
}

// evictIfFull drops the oldest entries down to the cap. Callers hold mu.
func (c *Collector) evictIfFull() {
	for len(c.workItemEntries) > trackedWorkItemsCap && len(c.workItemOrder) > 0 {
		victim := c.workItemOrder[0]
		c.workItemOrder = c.workItemOrder[1:]
		// delete is a no-op when the key is absent, so no guard
		// is needed (lazy ordering may carry dead refs).
		delete(c.workItemEntries, victim)
	}
}

func stringFromPayload(p map[string]any, key string) string {
	if p == nil {
		return ""
	}
	v, ok := p[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

func floatFromPayload(p map[string]any, key string) float64 {
	if p == nil {
		return 0
	}
	v, ok := p[key]
	if !ok {
		return 0
	}
	switch x := v.(type) {
	case float64:
		return x
	case float32:
		return float64(x)
	case int:
		return float64(x)
	case int64:
		return float64(x)
	}
	return 0
}
