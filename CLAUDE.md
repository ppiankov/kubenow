# Project: kubenow

Kubernetes cluster analysis tool. Three modes: monitor (real-time TUI), analyze (deterministic cost/capacity), pro-monitor (resource alignment with bounded apply).

## Commands
- `make build` — Build binary
- `make test` — Run tests with race detection
- `make lint` — Run golangci-lint
- `make fmt` — Format with gofmt/goimports
- `make vet` — Run go vet
- `make check` — All checks (fmt, vet, lint, test)
- `make clean` — Clean build artifacts

## Architecture
- Entry: `cmd/kubenow/main.go` (minimal, delegates to internal/)
- CLI: `internal/cli/` — Cobra commands
- Monitor: `internal/monitor/` — Real-time TUI (BubbleTea)
- Pro-Monitor: `internal/promonitor/` — Resource alignment engine
- Metrics: `internal/metrics/` — Latch (sub-scrape-interval spike monitoring)
- Policy: `internal/policy/` — Admin-owned guardrails
- Audit: `internal/audit/` — Immutable apply trail
- Analyzer: `internal/analyzer/` — Deterministic analysis (bin-packing, requests-skew)

## Safety Invariants
- UNSAFE rating structurally blocks recommendations (no numbers produced)
- Safety margins are multiplicative, not advisory
- Policy bounds cap changes per admin config
- Pre-flight checks gate all mutations (10+ conditions)
- Read-back verification detects post-apply drift
- Audit bundles record every apply attempt (success or denial)
- Connection failures MUST NOT display healthy state

## Conventions
- Minimal main.go — single Execute() call
- Internal packages: short single-word names
- Pure functions for core algorithms (no side effects)
- Input/output structs for all operations
- Version injected via LDFLAGS at build time
- BubbleTea for TUI — never write to stdout directly during TUI mode

## Anti-Patterns
- NEVER suppress API errors as empty results
- NEVER skip error handling — always check returned errors
- NEVER use init() functions unless absolutely necessary
- NEVER use global mutable state
- NEVER print to stdout during BubbleTea alternate screen mode

## Verification
- Run `make test` after code changes (includes -race)
- Run `make lint` before marking complete
- Run `go vet ./...` for suspicious constructs
