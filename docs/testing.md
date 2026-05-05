# Katty-Go Testing & Quality

Last run: 2026-05-06 | Binary: `katty` (11 MB, zero external deps)

## Quick Run

```bash
# All checks
./scripts/ci.sh

# Individual
gofmt -w . && gofmt -l .           # formatting
go vet ./...                        # basic static analysis
staticcheck ./...                   # advanced static analysis
golangci-lint run ./...             # aggregated lint
go test -race -cover ./...          # unit + race + coverage
go test -fuzz=. -fuzztime=10s ./... # fuzzing
go test -bench=. -benchmem ./...    # benchmarks
```

---

## 1. Formatting

**Command:** `gofmt -w . && gofmt -l .`

**Status:**Clean — zero files with formatting issues.

---

## 2. Static Analysis

| Tool | Status | Notes |
|------|--------|-------|
| `go vet ./...` |Clean | |
| `staticcheck ./...` |Clean | 5 issues fixed: unused helpers, redundant code, MCP cleanup |
| `golangci-lint run ./...` |Clean | `errcheck` disabled via `.golangci.yml` (deferred closes, best-effort kills, pprof setup — all intentional). All other linters active. |

### Fixed Issues

- `proc.go`: moved `procCtx.Err()` check before `*exec.ExitError` so timeouts return `IsError=true`
- `proc.go`: collapsed 3 redundant `cmd` assignments in `procPs`
- `tools.go`: removed dead `toJSON` + unused `encoding/json` import
- `mcp/manager.go`: simplified `len()` check on nil map
- `mcp/stdio_client.go`: removed unused `serverInfo` field

---

## 3. Module Hygiene

**Command:** `go mod tidy`

**Status:**Zero external dependencies. `go.sum` is empty — by design.

---

## 4. Unit Tests

**Command:** `go test -race -cover ./...`

| Package | Coverage | Tests |
|---------|----------|-------|
| `internal/config` | 96.3% | 5 |
| `internal/builtin` | 30.4% | 24 |
| `internal/repl` | 10.2% | 10 |

**Status:**All 39 tests pass.

### Test files

| File | What |
|------|------|
| `internal/builtin/builtin_test.go` | Tool registration, FS ops, proc exec, os probes, result helpers |
| `internal/config/config_test.go` | Defaults, missing files, path expansion, transcript dir |
| `internal/repl/repl_test.go` | Tool call parsing (1/multi/malformed/MCP), dangling action detection |

---

## 5. Fuzz Tests

**Command:** `go test -fuzz=. -fuzztime=10s ./...`

| Fuzzer | Package | Execs | New Coverage |
|--------|---------|-------|-------------|
| `FuzzParseToolCalls` | repl | 1.66M | 64 |
| `FuzzIsDanglingAction` | repl | 1.29M | 160 |
| `FuzzFsPatch` | builtin | 9.8K | 6 |
| `FuzzGetStr` | builtin | 923K | 3 |
| `FuzzGetInt` | builtin | 854K | 4 |

**Status:**No panics, crashes, or hangs across 4.7M+ executions.

---

## 6. Benchmarks

**Command:** `go test -bench=. -benchmem ./...`

Results saved to `benchmarks/2026-05-06.txt`.

### Key Latencies (Apple M3 Pro)

| Operation | Latency | Allocs | Memory |
|-----------|---------|--------|--------|
| `RegistryLookup` | **6 ns** | 0 | 0 B |
| `ErrResult` | **0.27 ns** | 0 | 0 B |
| `OkResult` | 18 ns | 0 | 0 B |
| `IsDanglingAction` | 147 ns | 2 | 62 B |
| `ParseToolCalls` (1 call) | 1.0 µs | 21 | 896 B |
| `ParseToolCalls` (3 calls) | 2.5 µs | 51 | 2.6 KB |
| `FsStat` | 1.6 µs | 10 | 1.2 KB |
| `FsList /tmp 100` | 1.9 µs | 15 | 1.2 KB |
| `FsRead 1KB` | 12.3 µs | 15 | 22.6 KB |
| `ToolRegistration` (31 tools) | 13.0 µs | 381 | 54.8 KB |
| `OsWhich` (3 bins) | 77 µs | 297 | 26.7 KB |
| `ProcExec echo` | 1.46 ms | 73 | 12.2 KB |
| `OsInfo` (fork uname) | 1.80 ms | 109 | 47.8 KB |

**Notes:** Process-fork operations (`ProcExec`, `OsInfo`) are I/O-bound at ~1.5 ms. All in-memory operations are sub-100 µs. The registry lookup is pure map access (6 ns, 0 allocs).

---

## 7. Race Detection

**Command:** `go test -race ./...`

**Status:**Clean — zero data races detected across all 39 tests. All packages pass with `-race` enabled.

---

## 8. Aggregated Lint (golangci-lint)

**Command:** `golangci-lint run ./...`

**Status:**0 issues. Config at `.golangci.yml` disables only `errcheck` (all unchecked errors are intentional — deferred `Close()`, `syscall.Kill`, `pprof` setup, `io.WriteString` to validated pipes).

## CI Script

`./scripts/ci.sh` runs all 7 checks in sequence and exits non-zero on any failure. Run with `--fix` to auto-apply `gofmt` before the format check.
