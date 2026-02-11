# Project: kubenow

Kubernetes cluster analysis tool. Three modes: monitor (real-time TUI), analyze (deterministic cost/capacity), pro-monitor (resource alignment with bounded apply).

## Philosophy: RootOps

Principiis obsta — resist the beginnings. Address root causes, not symptoms. Control over observability. Determinism over ML. Restraint over speed.

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
- Exposure: `internal/exposure/` — Structural traffic topology (services, ingresses, netpols, neighbors)
- Analyzer: `internal/analyzer/` — Deterministic analysis (bin-packing, requests-skew)

## Code Style
- Go: minimal main.go delegating to internal/, Cobra for CLIs, golangci-lint, race detection in tests
- Comments explain "why" not "what". No decorative comments
- No magic numbers — name and document constants
- Defensive coding: null checks, graceful degradation, fallback to defaults

## Naming
- Go files: snake_case.go
- Go packages: short single-word (cache, cli, model, worker)
- Conventional commits: feat:, fix:, docs:, test:, refactor:, chore:

## Testing
- Tests are mandatory for all new code. Coverage target: >85%
- Deterministic tests only — no flaky/probabilistic tests
- Go: -race flag always
- Test files alongside source
- TDD preferred: write tests first, then implement to make them pass

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

## Verification — IMPORTANT
- Run `make test` after code changes (includes -race)
- Run `make lint` before marking complete
- Run `go vet ./...` for suspicious constructs
- Never mark a task complete if tests fail or implementation is partial

## Git Safety — CRITICAL
- NEVER force push, reset --hard, or skip hooks (--no-verify) unless explicitly told
- NEVER commit secrets, binaries, backups, or generated files
- NEVER include Co-Authored-By lines in commits
- NEVER add "Generated with Claude Code" or emoji watermarks to PRs, commits, or code
- Small, focused commits over large monolithic ones

## Commit Messages
Format: `type: concise imperative statement` (lowercase after colon, no period)
Types: feat, fix, docs, test, refactor, chore, perf, ci, build
- ONE line. Max 72 chars. Say WHAT changed, not every detail of HOW

## Anti-Patterns
- NEVER suppress API errors as empty results
- NEVER skip error handling — always check returned errors
- NEVER use init() functions unless absolutely necessary
- NEVER use global mutable state
- NEVER print to stdout during BubbleTea alternate screen mode
- NEVER add features, refactor code, or make improvements beyond what was asked
- NEVER remove existing CI jobs when updating workflows — only add or modify
