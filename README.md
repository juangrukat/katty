# Katty-Go

> A trusted terminal workbench for model-driven systems work.

Katty-Go is a single-binary assistant runtime that lets a model work directly with a real Unix-like machine through typed native tools.

It is not a chatbot bolted onto a shell. It is a small operating surface for local and remote work: inspect files, patch code, run tests, keep sessions alive, probe the environment, copy files to targets, call MCP tools, and record what happened.

The core idea is simple: give the model better hands than raw shell strings.

---

## What It Is

Katty-Go starts an interactive terminal assistant backed by DeepSeek. On startup it builds a system context from:

* `~/.katty/config.json`
* `~/.katty/soul.md`
* `~/.katty/preferences.md`
* local OS and environment probes
* configured remote targets
* built-in tool definitions
* available MCP servers and tools

The model can then call Katty tools through a text-block protocol. Katty parses those calls, executes them, feeds the results back into the conversation, and continues the loop until there is a final answer.

This makes Katty useful for work that is naturally iterative:

```text
inspect -> edit -> run -> observe -> adjust -> verify
```

Examples:

* read a source file, patch it, run tests, and explain the result
* find where a config is defined and update it precisely
* start a dev server or REPL and keep interacting with it
* inspect a staging target, copy logs back, and summarize failures
* check whether required tools exist before choosing commands
* expose additional local capabilities through MCP servers

---

## Trust Model

Katty-Go assumes a trusted operator.

It does not sandbox filesystem access, wrap tool calls in a policy layer, or prevent dangerous-but-valid actions. If a tool is available to the model, the model can call it.

That is intentional. Katty is meant for environments where you control the machine, credentials, targets, and scope of work. For shared, production, or high-risk use, add isolation outside Katty: a restricted user, container, VM, disposable worktree, target-specific credentials, network controls, approvals, or snapshots.

---

## Quick Start

### Prerequisites

* Go 1.21+
* A DeepSeek API key in `DEEPSEEK_API_KEY`

### Build

```bash
git clone https://github.com/kat/katty.git
cd katty
go build -o katty .
```

On macOS, if you install the binary somewhere on your `PATH`, ad-hoc sign the copied binary so the OS does not kill it before startup:

```bash
mkdir -p ~/.local/bin
cp ./katty ~/.local/bin/katty
codesign --force --sign - ~/.local/bin/katty
hash -r
```

### Run

```bash
export DEEPSEEK_API_KEY="sk-..."

./katty
```

Useful startup modes:

```bash
./katty --doctor        # run diagnostics and exit
./katty --print-system  # print the assembled system prompt/context
./katty --no-mcp        # skip configured MCP server startup
./katty --config path   # use a specific config file
```

On first run, Katty uses `~/.katty/config.json` if it exists. If it does not, Katty writes a default config. Startup instruction files live beside it:

```text
~/.katty/config.json
~/.katty/soul.md
~/.katty/preferences.md
~/.katty/sessions/
```

---

## How It Feels To Use

You can ask for outcomes, not just commands:

```text
katty> find the failing auth test and fix the smallest thing
```

Katty can search the repo, read the relevant files, patch the code, run the test, and report what changed.

For remote work:

```text
katty> check nginx on staging and show me the most relevant logs
```

Katty can inspect configured targets, run a remote command, copy or read logs, and summarize what matters.

For long-running work:

```text
katty> start the dev server and watch for errors while I test it
```

Katty can create a persistent session, read buffered output, send input later, and stop it when no longer needed.

The useful loop is not "ask, answer, done." It is a working loop with machine feedback.

---

## Built-In Tools

Katty-Go ships with native tools under the `katty` server.

### Filesystem: `katty.fs.*`

```text
list read write append patch copy move remove mkdir stat search glob
```

Use these to inspect and change files without forcing every operation through shell syntax. `search` uses `rg` when available. `patch` performs exact text replacement, which is useful for precise edits where broad rewriting would be risky.

### Process: `katty.proc.*`

```text
exec ps signal
```

Use these to run bounded commands, inspect processes, and send signals. Command execution supports stdin, timeouts, process groups, and captured stdout/stderr.

### Session: `katty.session.*`

```text
start send read stop list
```

Use sessions for dev servers, REPLs, debuggers, log tails, watchers, and other long-running workflows. Output is buffered so the assistant can check progress without losing context.

### Target: `katty.target.*`

```text
list info ping exec copy_to copy_from
```

Use targets for configured remote machines. The target layer uses system `ssh`, `scp`, and `rsync`, so it fits naturally into existing operator workflows and credentials.

### OS: `katty.os.*`

```text
info detect which capabilities
```

Use these to detect the local environment before assuming package managers, userland behavior, service managers, or available commands.

### Network: `katty.net.*`

```text
check
```

Use this for host and port reachability checks.

---

## Configuration

Katty's default config is intentionally small and lives at `~/.katty/config.json`.

Important sections:

* `model`: DeepSeek model, API base URL, API key environment variable, request timeout
* `startup`: files loaded into the system context, usually `soul.md` and `preferences.md`
* `environment`: local command checks used for capability probing
* `capabilities`: named command families Katty reports to the model
* `targets`: local and remote machines available to `katty.target.*`
* `mcp_servers`: optional external MCP servers
* `tooling`: tool-loop behavior such as max rounds
* `output`: result-size, formatting, and terminal passthrough preferences
* `transcripts`: JSONL session log location

The startup files are ordinary Markdown. They are where you put durable behavior, preferences, conventions, and machine-operation guidance that should shape every session.

For raw terminal use, prefix a command with `!`:

```text
katty> !ls -la ~/Desktop
```

That path bypasses DeepSeek entirely. Katty runs the command through your shell, prints stdout/stderr directly, does not append anything to conversation history, and returns to the prompt.

For natural-language terminal display requests, the model can mark a tool call with `final: true`. When `output.terminal_passthrough` is enabled, Katty prints stdout/stderr directly, stores only a small tombstone result in history, and stops the turn instead of spending another model call interpreting the output.

Display-only requests also apply to structured Katty tools where the result is naturally terminal-shaped. For example, `what files are on my desktop` can use `katty.fs.list` and print a plain names-only listing instead of dumping JSON or asking the model to summarize the list.

---

## MCP Extensibility

Katty-Go can start and call configured MCP servers.

Example:

```json
{
  "mcp_servers": {
    "file-utils": {
      "enabled": true,
      "required": false,
      "transport": "stdio",
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"],
      "cwd": "~/mcp",
      "startup_timeout_seconds": 10,
      "call_timeout_seconds": 60
    }
  }
}
```

Enabled servers are started during initialization. Optional failures are reported as warnings; required failures stop startup. Only initialized servers contribute tools to the model context.

---

## Tool Call Protocol

Katty uses text-block tool calls so the runtime is not tied to provider-native function calling.

The model emits:

```xml
<katty_tool_call>
{"server":"katty","tool":"katty.proc.exec","args":{"cmd":"/bin/ls","args":["-la","/tmp"]},"final":true}
</katty_tool_call>
```

Katty parses the block, executes the tool, and either continues the loop or ends the turn depending on `final`.

Use `final: true` when stdout/stderr is itself the answer. Leave it unset or false when the output is input to further reasoning, debugging, fixing, or summarizing.

Parser behavior is defensive:

| Scenario | Behavior |
| --- | --- |
| Malformed JSON | skipped without crashing |
| Partial tool blocks | ignored |
| Multiple calls | parsed and executed sequentially |
| Nested blocks | outer block wins |
| Dangling action phrases | nudged toward a concrete tool call |
| Tool args | JSON-decoded, not eval'd |

The tool catalog in the system prompt is authoritative. If a tool is not listed there, the model is instructed not to invent it.

---

## Architecture

```text
katty
├── deepseek        DeepSeek chat client
├── repl            interactive controller and tool loop
├── builtin         native filesystem/process/session/target/OS/network tools
├── mcp             MCP manager and stdio client
├── config          default config, loading, and path expansion
├── startup         startup file loading and first-run directory setup
├── systemctx       system prompt assembly
├── envprobe        OS, user, shell, and command discovery
└── transcript      JSONL session logging
```

The project is deliberately compact and dependency-light. Most capabilities use Go's standard library plus the host system tools already present on the machine.

---

## REPL Commands

```text
/help, /env, /capabilities, /files, /targets
/mcp, /tools, /schema <tool>, /tool <tool> <json>
/interrupt, /ps, /sessions, /kill
/reset-terminal, /reload, /system, /doctor
/debug messages, /exit
```

---

## Quality

Run the full local check suite:

```bash
./scripts/ci.sh
```

The suite covers formatting, vetting, static analysis, linting, race-enabled tests, fuzz smoke tests, and benchmark smoke tests.

Benchmark snapshots live in `benchmarks/`. Additional testing notes live in `docs/testing.md`.

---

## Current Limitations

* DeepSeek is the built-in provider in v1.
* Tool calls are text-block based, not provider-native function calls.
* There is no built-in safety policy or sandbox.
* Remote targets use system `ssh`, `scp`, and `rsync`.
* MCP support is intentionally minimal.
* Responses are not streamed.
* There is no long-term memory beyond startup files and transcripts.

---

## License

MIT
