// Pool daemon wiring (gm-s47n.12). Loads the [pool] config, computes
// the MaxParallel clamp, and constructs one autodispatch.Daemon per
// (rig, persona) pool with size > 0. Phase 0 zero-delta: when no
// pool config exists the loader returns an empty slice and no
// daemons are constructed.

package cli

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/adapter/native/agents"
	"github.com/GembaCore/gemba-core/internal/config"
	"github.com/GembaCore/gemba-core/internal/planner"
	"github.com/GembaCore/gemba-core/internal/planner/autodispatch"
	"github.com/GembaCore/gemba-core/internal/server"
	"github.com/GembaCore/gemba-core/internal/transport/api"
)

// DefaultDaemonTickPeriod is the period the autodispatch daemon's
// loop runs at when no per-pool override is set. Spec §6 doesn't
// pin a number; 10s matches `work-planning.md`'s "loop runs every
// few seconds" prose without thrashing on a quiet rig.
const DefaultDaemonTickPeriod = 10 * time.Second

// loadAndResolvePools reads the pool config (if any) and resolves
// the cascade. Returns (nil, zero PoolConfig, nil) for the Phase 0
// zero-delta path — every existing serve test stays green because
// the rest of the wiring is gated on len(resolved) > 0.
//
// Resolution order:
//  1. cfg.PoolConfigPath, when non-empty — explicit path
//  2. .gemba/pool.toml in cwd — convention
//  3. neither exists → empty config (no daemons)
//
// MaxParallel is derived from the .gemba/agents.toml registry:
// the maximum max_parallel across all configured agents. Empty
// registry → 0 (no clamp).
func loadAndResolvePools(cfg config.ServeConfig) ([]config.ResolvedPool, config.PoolConfig, error) {
	path := cfg.PoolConfigPath
	if path == "" {
		// Probe the conventional location.
		if cwd, err := os.Getwd(); err == nil {
			candidate := filepath.Join(cwd, ".gemba", "pool.toml")
			if _, err := os.Stat(candidate); err == nil {
				path = candidate
			}
		}
	}
	poolCfg, err := config.LoadPoolConfig(path)
	if err != nil {
		return nil, config.PoolConfig{}, err
	}
	maxParallel := loadMaxParallel(cfg)
	resolved := poolCfg.Resolve(maxParallel)
	reserved := poolCfg.ReservedForManual
	if reserved <= 0 {
		reserved = config.DefaultReservedForManual
	}
	config.LogClampWarnings(resolved, maxParallel, reserved)
	if len(resolved) > 0 {
		slog.Info("pool: daemon construction set",
			"path", path,
			"pools", len(resolved),
			"max_parallel", maxParallel,
			"reserved_for_manual", reserved)
	}
	return resolved, poolCfg, nil
}

// loadMaxParallel returns the maximum agents.toml `max_parallel` on
// the host. Empty / unreadable registry → 0 (no clamp). The clamp
// is best-effort: an operator running with --orchestration=none has
// no agent registry but may still configure pools, in which case the
// clamp is silently disabled.
func loadMaxParallel(cfg config.ServeConfig) int {
	registryPath := cfg.AgentsRegistryPath
	if registryPath == "" {
		registryPath = ".gemba/agents.toml"
	}
	reg, err := agents.Load(registryPath)
	if err != nil {
		return 0
	}
	max := 0
	for _, a := range reg.Agents {
		if mp := a.ResolvedMaxParallel(); mp > max {
			max = mp
		}
	}
	return max
}

// startPoolDaemons constructs one autodispatch.Daemon per resolved
// pool and runs each on its own goroutine. The daemons share the
// bound OrchestrationPlane + WorkPlane; their per-pool isolation
// comes from the IdleSessionLister + ReadySetReader filters
// (spec §6.1, §6.3).
//
// Also wires the session_recycles writer that subscribes to the
// OrchestrationPlane's `session.recycled` events and persists rows
// to the dolt audit table (spec §10.3).
//
// Best-effort: a per-pool wiring failure is logged but does NOT
// abort startup. Other pools keep running; the failed pool's beads
// fall back to manual drag.
func startPoolDaemons(
	ctx context.Context,
	router *server.Router,
	host *api.Host,
	resolved []config.ResolvedPool,
	poolCfg config.PoolConfig,
) {
	op := host.OrchestrationPlane()
	wp := host.WorkPlane()
	if op == nil {
		slog.Warn("pool: no orchestration plane bound; daemons not started")
		return
	}
	for _, p := range resolved {
		daemon, err := buildDaemon(op, wp, p, poolCfg)
		if err != nil {
			slog.Warn("pool: daemon build failed",
				"rig", p.Rig, "persona", p.Persona, "err", err)
			continue
		}
		go runDaemon(ctx, p, daemon)
	}
}

func runDaemon(ctx context.Context, p config.ResolvedPool, d *autodispatch.Daemon) {
	period := DefaultDaemonTickPeriod
	slog.Info("pool: daemon started",
		"rig", p.Rig,
		"persona", p.Persona,
		"agent_type", p.AgentType,
		"size", p.SizeEffective,
		"floor", p.Floor,
		"period", period)
	if err := d.Run(ctx, period); err != nil && ctx.Err() == nil {
		slog.Warn("pool: daemon exited",
			"rig", p.Rig, "persona", p.Persona, "err", err)
	}
}

// buildDaemon assembles one autodispatch.Daemon bound to one pool.
// Per-pool DispatchGate uses the pool's MinIntervalSeconds +
// MaxConcurrent overrides (cascading to planner defaults via the
// gate's resolved() method).
func buildDaemon(
	op core.OrchestrationPlaneAdaptor,
	wp core.WorkPlane,
	p config.ResolvedPool,
	cfg config.PoolConfig,
) (*autodispatch.Daemon, error) {
	if op == nil {
		return nil, errors.New("orchestration plane is nil")
	}
	w := server.NewPoolWiring(
		op,
		wp,
		p,
		cfg,
		planner.OperationalContextReaders{}, // best-effort; profile/health degrade to nil
		p.AgentType,
	)
	policy := planner.DispatchPolicy{
		Enabled:               true,
		MinIntervalPerSession: time.Duration(p.MinIntervalSeconds) * time.Second,
		MaxConcurrent:         p.MaxConcurrent,
	}
	gate := planner.NewDispatchGate(policy)
	return &autodispatch.Daemon{
		Idle:              w.IdleLister(),
		Live:              w.LiveLister(),
		Ready:             w.ReadyReader(),
		Dispatcher:        w.Dispatcher(),
		Recycler:          w.Recycler(),
		Gate:              gate,
		Logger:            slog.Default(),
		AutoDispatchFloor: p.Floor,
	}, nil
}
