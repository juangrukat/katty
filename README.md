# Katty-Go

> **Native system tools. DeepSeek precision. Your terminal, your rules.**

Katty-Go is not an AI shell wrapper. It is a native operations substrate for model-driven terminal work — 31 system tools for filesystem, process, session, target, OS, and network operations in a single, zero-dependency Go binary.

It doesn't delegate to a shell. It doesn't wrap safety policies. It gives you granular primitives that assume operator trust and reward precision.

---

## Who It's For

You're working directly on production machines. You need `katty.fs.patch` to surgically replace a config line, `katty.session.start` to hold a long-running debugger, `katty.target.exec` to run a command on the staging box — all from one terminal, all through DeepSeek, all with zero shell delegation.

Katty-Go is built for **systems programmers and toolmakers** who want sharp tools, not training wheels.

## Trust Model

Katty-Go is designed for trusted operators working on trusted local or remote machines. It does not sandbox commands, rewrite prompts through a safety layer, or restrict filesystem/process access by default.

Use it when you want precise primitives and direct control. Do not expose it to untrusted users, untrusted prompts, or shared multi-tenant environments without additional isolation.

---

## First Session

```bash
$ ./katty
katty> check whether nginx is running on staging
```

Katty-Go calls `katty.target.exec` against your `staging` target, runs `systemctl status nginx`, and reports the result.

```
katty> patch the staging config to increase worker_connections from 1024 to 2048
```

It fetches `/etc/nginx/nginx.conf` via `katty.target.copy_from`, applies `katty.fs.patch` with exact old/new text, and pushes the result back with `katty.target.copy_to`.

```
katty> restart nginx on staging and show me the logs
```

It runs `systemctl restart nginx` on the target, then tails the error log through a `katty.session.start` session — all in one turn.

That's the loop: observe, patch, push, restart, verify. No shell delegation. No safety prompts. Just tools and trust.

Because Katty-Go assumes operator trust, workflows like this should be run from a context where target names, credentials, and write permissions are already intentionally scoped.

---

## Performance

Single binary. The core dispatch path — registry lookup, result construction, and dangling-action detection — is allocation-free. I/O-heavy tools (filesystem reads, process forks) remain bounded by the OS, not by Katty-Go overhead. Everything in-memory is sub-100 µs; everything that touches the kernel is ~1.5 ms.

| Operation | Latency | Allocs | Memory |
|-----------|---------|--------|--------|
| Registry lookup (31 tools) | **6 ns** | 0 | 0 B |
| Tool result construction | **18 ns** | 0 | 0 B |
| Dangling-action detection | 147 ns | 2 | 62 B |
| Parse 1 tool call from model output | 1.0 µs | 21 | 896 B |
| Parse 3 tool calls | 2.5 µs | 51 | 2.6 KB |
| `fs.stat` | 1.6 µs | 10 | 1.2 KB |
| `fs.list` (100 entries) | 1.9 µs | 15 | 1.2 KB |
| `fs.read` (1 KB) | 12.3 µs | 15 | 22.6 KB |
| `os.which` (3 binaries) | 77 µs | 297 | 26.7 KB |
| `proc.exec` (echo) | 1.46 ms | 73 | 12.2 KB |
| `os.info` (forks `uname`) | 1.80 ms | 109 | 47.8 KB |

*Apple M3 Pro. Full benchmark suite: `go test -bench=. -benchmem ./...` — results archived in `benchmarks/`.*

---

## Quick Start

### Prerequisites
- Go 1.21+
- A [DeepSeek API key](https://platform.deepseek.com/)

### Build
```bash
git clone https://github.com/kat/katty.git
cd katty
go build -o katty .
```

### Configure
```bash
export DEEPSEEK_API_KEY="sk-..."
```

No config file needed — Katty-Go ships with sensible defaults. Optionally customize at `~/.katty/config.json`.

### Run
```bash
./katty                     # start the assistant
./katty --doctor            # run diagnostics (31 tools, 11 capability families)
./katty --print-system      # print system context
./katty --no-mcp            # skip MCP server startup
```

---

## Built-in Tools (31)

All tools live under the `katty` server and are called via DeepSeek's tool-call loop.

### Filesystem (`katty.fs.*`)
`list` `read` `write` `append` `patch` `copy` `move` `remove` `mkdir` `stat` `search` `glob`
— surgical file editing with `patch` for exact find-and-replace, `search` delegates to `rg` when available.

### Process (`katty.proc.*`)
`exec` `ps` `signal`
— command execution with timeout, stdin, process groups, and clean Ctrl-C handling.

### Session (`katty.session.*`)
`start` `send` `read` `stop` `list`
— long-running process sessions (debuggers, REPLs, tails) with buffered I/O.

### Target (`katty.target.*`)
`list` `info` `ping` `exec` `copy_to` `copy_from`
— remote machine operations via system `ssh`/`scp`/`rsync`.

```bash
# List configured targets
/targets

# Check staging box health
/tool katty.target.exec {"target":"staging","cmd":["systemctl","status","nginx"],"timeout_ms":5000}

# Push a config to production
/tool katty.target.copy_to {"target":"prod","src":"/tmp/nginx.conf","dst":"/etc/nginx/nginx.conf"}

# Pull logs back for inspection
/tool katty.target.copy_from {"target":"prod","src":"/var/log/nginx/error.log","dst":"/tmp/prod-error.log"}
```

### OS (`katty.os.*`)
`info` `detect` `which` `capabilities`
— environment introspection and binary discovery.

### Network (`katty.net.*`)
`check`
— host/port reachability.

---

## Architecture

```
katty (11 MB, single binary)
├── deepseek        API client with auto tool-call loop
├── builtin         31 native tools (no shell delegation)
├── mcp             Model Context Protocol client (stdio + HTTP)
├── config          ~/.katty/config.json with path expansion
├── systemctx       OS/capability/environment probe
├── startup         Loads soul.md + preferences.md into system prompt
├── transcript      JSONL session logging
└── repl            Interactive loop with Ctrl-C reliability
```

**Zero external dependencies.** `go.sum` is empty by design.

DeepSeek is the built-in provider in v1. The text-block tool-call protocol keeps the tool loop independent of provider-native function calling — swap the model client without touching the tools.

---

## Tool Call Protocol

The model emits tool calls in a text block — no provider-native function calling needed:

```
<katty_tool_call>
{"server":"katty","tool":"katty.fs.list","args":{"path":"/tmp","max_entries":100}}
</katty_tool_call>
```

Katty-Go handles both fully-qualified (`katty.fs.list`) and short (`fs.list`) tool names from the model. MCP tools use the configured server prefix (e.g., `{"server":"file-utils","tool":"catalog_run_tool",...}`).

### Tool-call Parsing

Because tool calls arrive as raw model text — not a structured API — the parser is defensive by design:

| Scenario | Behavior |
|----------|----------|
| **Malformed JSON** | Silently skipped; no crash, no garbage tool call |
| **Partial / truncated blocks** | Incomplete `<katty_tool_call>...</katty_tool_call>` pairs are ignored |
| **Multiple calls in one response** | All parsed; executed sequentially, results concatenated |
| **Nested or overlapping blocks** | Outer block wins; inner is treated as text |
| **Dangling action phrases** | If the model says *"Let me check that"* but emits no tool call, Katty-Go detects the dangling intent and nudges for a concrete call |
| **Code injection via tool args** | Args are JSON-decoded, not `eval`'d — string values stay strings |
| **Dangerous-but-valid calls** | Katty-Go does not classify user intent or prevent dangerous tool calls — that responsibility stays with the operator |

The parser is fuzz-tested against 1.6M+ random inputs and handles the full range of model output weirdness without panicking.

---

## MCP Extensibility

Katty-Go is an MCP client. Add external tool servers in `~/.katty/config.json`:

```json
{
  "mcp_servers": {
    "file-utils": {
      "enabled": true,
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"],
      "cwd": "~/mcp"
    }
  }
}
```

- MCP servers start automatically when enabled
- Tool calls only route to live, initialized clients
- Optional server failures are logged; required failures stop startup
- Stdio and HTTP transports supported

---

## Startup Files

Katty-Go loads personality and preferences from `~/.katty/`:

| File | Purpose |
|------|---------|
| `soul.md` | Core personality and behavior instructions |
| `preferences.md` | User preferences, conventions, project context |

These are injected into the system prompt and influence all model interactions.

---

## Terminal Reliability

- **Ctrl-C once** → cancels current operation, returns to prompt
- **Ctrl-C twice within 2s** → exits cleanly
- Child processes never inherit terminal stdin
- Process groups ensure clean timeout/termination (`Setpgid: true`)
- MCP servers survive individual Ctrl-C events

---

## REPL Commands

```
/help, /env, /capabilities, /files, /targets
/mcp, /tools, /schema <tool>, /tool <tool> <json>
/interrupt, /ps, /sessions, /kill
/reset-terminal, /reload, /system, /doctor
/debug messages, /exit
```

---

## Quality

| Check | Status |
|-------|--------|
| `gofmt` |clean |
| `go vet` |clean |
| `staticcheck` |clean |
| `golangci-lint` |0 issues |
| `go test -race -cover` |39 tests, 0 races |
| Fuzz (5 fuzzers, 4.7M+ execs) |no panics |
| Benchmarks (13 ops) | ✅ |

Run it yourself: `./scripts/ci.sh`

Full report: [`docs/testing.md`](docs/testing.md)

---

## Current Limitations (v1)

- Text-block tool calls, not provider-native function calling
- No safety policy — assumes trusted local/remote targets
- Serializes MCP calls per server
- No native Go SSH — uses system `ssh`/`scp`/`rsync`
- No raw syscall wrappers (`mmap`/`ptrace`/`ioctl`) — use generated probes
- No streaming responses
- No long-term memory

---

## License

MIT
