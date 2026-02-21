# Work Orders — Security Audit Remediation

Generated from comprehensive security audit (2026-02-21).
5 parallel audits: input validation, secrets/auth, K8s API/network, deps/CI, error handling/data integrity.

**Findings**: 4 CRITICAL, 6 HIGH, 9 MEDIUM, 5 LOW

**Status**: ALL 24 WORK ORDERS COMPLETED (2026-02-21)

---

## CRITICAL

### WO-SEC-01: Fix PromQL injection in all query construction

**Severity**: CRITICAL
**Files**:
- `internal/metrics/query.go` (25 sites)
- `internal/metrics/prometheus.go` (1 site)
- `internal/metrics/discovery.go` (5 sites)
- `internal/exposure/collector.go` (8 sites)

**Problem**: User-controlled namespace and workload names are embedded directly into PromQL queries via `fmt.Sprintf` and string concatenation without escaping. The existing `quote()` helper (query.go:129-132) only wraps in double quotes — it does not escape `"`, `\`, or newlines within the value. A malicious namespace like `prod",namespace="admin` breaks out of the label matcher.

**Fix**:
1. Replace `quote()` with `escapePromQL(s string) string` that escapes `\` → `\\`, `"` → `\"`, `\n` → `\\n`
2. Apply `escapePromQL()` to all 38 interpolation sites across 4 files
3. For regex matchers (`pod=~`), add `escapePromQLRegex()` that also escapes `.`, `*`, `+`, `?`, `(`, `)`, `[`, `]`, `{`, `}`, `|`, `^`, `$`

**Verify**: `make test`, add unit test for namespace containing `"}` and workload containing `.*`

---

### WO-SEC-02: Pin Trivy GitHub Action to SHA

**Severity**: CRITICAL
**Files**: `.github/workflows/ci.yml:96`

**Problem**: `aquasecurity/trivy-action@master` uses a mutable branch reference. The security scanner itself becomes a supply chain attack vector — anyone with push access to the Trivy repo can inject malicious code.

**Fix**: Pin to SHA of latest release (e.g., `aquasecurity/trivy-action@<sha>`)

**Verify**: CI passes on next push

---

### WO-SEC-03: Handle audit FinalizeBundle errors

**Severity**: CRITICAL
**Files**: `internal/promonitor/apply.go:657`

**Problem**: `_ = audit.FinalizeBundle(bundle, afterObj, status, ts, applyResult.Error)` silently discards the error. If FinalizeBundle fails (disk full, permission denied), the audit trail is incomplete: no after.yaml, no diff.patch, no finalized decision.json. Apply proceeds but audit contract is broken.

**Fix**:
1. Capture the error: `if err := audit.FinalizeBundle(...); err != nil {`
2. Log to stderr: `fmt.Fprintf(os.Stderr, "warning: audit finalization failed: %v\n", err)`
3. Set warning on applyResult so callers can surface it

**Verify**: `make test`

---

### WO-SEC-04: Remove global mutable SilentMode

**Severity**: CRITICAL
**Files**:
- `internal/analyzer/requests_skew.go:18` (declaration)
- `internal/cli/analyze_requests_skew.go:151` (write)
- `internal/cli/analyze_node_footprint.go:86` (write)

**Problem**: `var SilentMode = false` is package-level mutable state written from CLI commands and read from analyzer functions. Race condition under concurrent use. Violates CLAUDE.md anti-pattern: "NEVER use global mutable state."

**Fix**: Move `SilentMode` into the `RequestsSkewConfig` struct as a `Silent bool` field. Pass through the call chain. Remove the package-level variable.

**Verify**: `make test` (tests already run with `-race` flag)

---

## HIGH

### WO-SEC-05: Add context timeouts for Prometheus calls

**Severity**: HIGH
**Files**: `internal/cli/analyze_requests_skew.go:249,260,321`

**Problem**: A single `context.Background()` is created on line 249 and reused for health check, metric discovery, and full analysis — all blocking Prometheus operations. If Prometheus is slow or unresponsive, the command hangs indefinitely. The `prometheusTimeout` config value (line 218) is parsed but never applied to the context.

**Fix**: Replace `ctx := context.Background()` with `ctx, cancel := context.WithTimeout(context.Background(), timeout)` using the existing parsed timeout. Defer `cancel()`.

**Verify**: `make test`

---

### WO-SEC-06: Validate Prometheus URL

**Severity**: HIGH
**Files**: `internal/metrics/prometheus.go:37-51`

**Problem**: User-provided Prometheus URL is accepted without validation. Accepts `http://` (no TLS), `file://` scheme, cloud metadata endpoints (`169.254.169.254`), and internal service URLs. SSRF risk.

**Fix**:
1. Parse URL with `url.Parse()`
2. Reject `file://` scheme
3. Reject `169.254.x.x` and `127.0.0.1` / `localhost` unless `--allow-insecure` flag
4. Warn on `http://` (non-TLS)

**Verify**: Unit test with malicious URLs

---

### WO-SEC-07: Handle ignored io.ReadAll error

**Severity**: HIGH
**Files**: `internal/llm/client.go:97`

**Problem**: `body, _ := io.ReadAll(resp.Body)` — error discarded. If read fails, empty body is used, masking API errors. Error messages on line 101 include raw response body which could echo the Bearer token from line 88.

**Fix**:
1. Check error: `body, err := io.ReadAll(resp.Body); if err != nil { return "", fmt.Errorf("reading response: %w", err) }`
2. Truncate body in error messages to 500 chars max to limit exposure

**Verify**: `make test`

---

### WO-SEC-08: Handle ignored yaml.Marshal error

**Severity**: HIGH
**Files**: `internal/promonitor/export.go:137`

**Problem**: `data, _ := yaml.Marshal(doc)` — if marshaling fails, nil data is appended to the string builder, producing corrupted/empty YAML patches that could silently fail when applied.

**Fix**: Check error, propagate to caller. `exportPatch()` should return `(string, error)`.

**Verify**: `make test`

---

### WO-SEC-09: Add go mod verify to release workflow

**Severity**: HIGH
**Files**: `.github/workflows/release.yml:53`

**Problem**: `go mod download` runs without `go mod verify`. Downloaded modules are not validated against go.sum checksums before being compiled into release binaries.

**Fix**: Add `go mod verify` step after `go mod download`

**Verify**: CI passes

---

### WO-SEC-10: SHA-pin third-party GitHub Actions

**Severity**: HIGH
**Files**: `.github/workflows/ci.yml`, `.github/workflows/release.yml`

**Problem**: Third-party actions use mutable version tags:
- `codecov/codecov-action@v3`
- `golangci/golangci-lint-action@v7`
- `softprops/action-gh-release@v2`

**Fix**: Pin each to full commit SHA. Add comment with human-readable version.

**Verify**: CI passes

---

## MEDIUM

### WO-SEC-11: Add regex DoS protection

**Severity**: MEDIUM
**Files**: `internal/analyzer/requests_skew.go:333,392-404`

**Problem**: `--namespace-regex` flag compiled without length/complexity limits (line 333). `matchesAnyPattern()` ignores `regexp.MatchString` error (line 398), causing false negatives in namespace filtering.

**Fix**:
1. Cap pattern length at 256 characters
2. Check error from `regexp.MatchString()` — return error or treat as non-match with warning
3. Pre-compile patterns once rather than per-call

**Verify**: `make test`

---

### WO-SEC-12: Validate policy file path

**Severity**: MEDIUM
**Files**: `internal/policy/policy.go:297-305`

**Problem**: `resolvePath()` returns user-provided path or env var without validation. Accepts `../../../etc/shadow` or absolute paths to sensitive files. Used at line 106 for `os.ReadFile(path)`.

**Fix**: Apply `filepath.Clean()`, reject paths containing `..` components.

**Verify**: Unit test with traversal paths

---

### WO-SEC-13: Tighten audit file permissions to 0600

**Severity**: MEDIUM
**Files**: `internal/audit/bundle.go:144,155,181,203,228`

**Problem**: Audit files (before.yaml, decision.json, after.yaml, diff.patch) written with 0644 (world-readable). Contains workload names, resource configurations, and cluster information.

**Fix**: Change all `os.WriteFile(..., 0644)` → `os.WriteFile(..., 0600)` in bundle.go

**Verify**: `make test`, manually verify file permissions after apply

---

### WO-SEC-14: Return errors from deepCopyMap

**Severity**: MEDIUM
**Files**: `internal/audit/bundle.go:276-290`

**Problem**: `deepCopyMap()` returns `nil` on JSON marshal/unmarshal errors, silently losing Kubernetes object state. Callers (CreateBundle line 136, FinalizeBundle line 173) receive nil without knowing the copy failed.

**Fix**: Change signature to `func deepCopyMap(src map[string]interface{}) (map[string]interface{}, error)`. Update callers to check error.

**Verify**: `make test`

---

### WO-SEC-15: Add -trimpath to builds

**Severity**: MEDIUM
**Files**: `Makefile:7,26`, `.github/workflows/release.yml:84,87`

**Problem**: Release and dev binaries contain absolute build paths (e.g., `/Users/dev/kubenow/internal/...`), leaking developer filesystem structure.

**Fix**: Add `-trimpath` flag to all `go build` commands in Makefile and release workflow.

**Verify**: `make build`, inspect binary with `go version -m ./bin/kubenow`

---

### WO-SEC-16: Cap latch sample buffer

**Severity**: MEDIUM
**Files**: `internal/metrics/latch.go:338-339`

**Problem**: `CPUSamples` and `MemSamples` slices grow unbounded. At 1s interval for 24h = 86,400 samples per workload. With 500 workloads = ~345 MB for CPU samples alone.

**Fix**: Add max sample count constant (17,280 = 24h at 5s default). When exceeded, drop oldest samples (ring buffer or slice trimming).

**Verify**: `make test`, add test for buffer cap enforcement

---

### WO-SEC-17: Bounds-check ParseDuration

**Severity**: MEDIUM
**Files**: `internal/metrics/query.go:191-220`

**Problem**: `ParseDuration` accepts unbounded values. `999999999d` overflows `time.Duration`. Negative values (`-1d`) produce negative durations. No validation after parsing.

**Fix**:
1. Reject negative values
2. Cap at 365 days
3. Check for overflow before multiplication

**Verify**: Unit test with edge cases (`-1d`, `999999d`, `0s`, `366d`)

---

### WO-SEC-18: Scope release workflow permissions

**Severity**: MEDIUM
**Files**: `.github/workflows/release.yml:14-15`

**Problem**: `permissions: { contents: write }` at workflow level grants write access to all jobs. Only the release job needs write permissions.

**Fix**: Remove workflow-level permissions. Add `permissions: { contents: write }` only to the release job.

**Verify**: CI passes

---

### WO-SEC-19: Fix LDFLAGS version injection

**Severity**: MEDIUM
**Files**: `Makefile:6-7`, `.github/workflows/release.yml:64`

**Problem**: LDFLAGS uses `main.Version` (uppercase) with `v` prefix. CLAUDE.md requires `main.version` (lowercase) and `VERSION_NUM` (no `v` prefix): `main.version=${VERSION_NUM}`.

**Fix**:
- Makefile: Strip `v` prefix, use lowercase `main.version`
- release.yml line 64: Change to `-X main.version=${VERSION_NUM}`

**Verify**: `make build && ./bin/kubenow version`

---

## LOW

### WO-SEC-20: Log best-effort audit failures

**Severity**: LOW
**Files**: `internal/audit/ratelimit.go:86,90`

**Problem**: `_ = recordEntry(...)` silently discards errors when rate limit recording fails. Admin has no visibility that audit recording failed.

**Fix**: Replace `_ =` with error check and `fmt.Fprintf(os.Stderr, ...)` warning.

**Verify**: `make test`

---

### WO-SEC-21: Tighten latch persistence permissions

**Severity**: LOW
**Files**: `internal/promonitor/persistence.go:71`

**Problem**: Latch data files written with 0644 (world-readable). Contains resource usage patterns.

**Fix**: Change to 0600.

**Verify**: `make test`

---

### WO-SEC-22: Tighten export file permissions

**Severity**: LOW
**Files**: `internal/cli/analyze_requests_skew.go:511,531,552,1335`, `internal/cli/analyze_node_footprint.go:191`

**Problem**: Exported CSV/JSON/HTML analysis files written with 0644. Contains workload names, resource data, and infrastructure information.

**Fix**: Change to 0600.

**Verify**: `make test`

---

### WO-SEC-23: Add binary signing to releases

**Severity**: LOW
**Files**: `.github/workflows/release.yml`

**Problem**: Releases include SHA256 checksums but no GPG signatures or SLSA provenance. Users cannot cryptographically verify release authenticity.

**Fix**: Add GPG signing step using repository secret. Consider SLSA provenance generation.

**Verify**: Release workflow dry-run

---

### WO-SEC-24: Validate OPENAI_API_KEY format

**Severity**: LOW
**Files**: `internal/llm/client.go:58`

**Problem**: API key from environment accepted without format validation. No warning if empty or obviously invalid.

**Fix**: Validate minimum length (e.g., 10 chars). Warn if key looks malformed.

**Verify**: `make test`
