package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
	"sort"
	"strings"

	"github.com/kat/katty/internal/builtin"
	"github.com/kat/katty/internal/config"
	"github.com/kat/katty/internal/deepseek"
	"github.com/kat/katty/internal/envprobe"
	"github.com/kat/katty/internal/mcp"
	"github.com/kat/katty/internal/repl"
	"github.com/kat/katty/internal/startup"
	"github.com/kat/katty/internal/systemctx"
	"github.com/kat/katty/internal/transcript"
)

var (
	configPath  = flag.String("config", "", "Path to config file")
	printSystem = flag.Bool("print-system", false, "Print system context and exit")
	doctorMode  = flag.Bool("doctor", false, "Run diagnostics and exit")
	noMCP       = flag.Bool("no-mcp", false, "Disable MCP servers")
	profileAddr = flag.String("profile", "", "Start pprof HTTP server on addr (e.g. :6060)")
	traceFile   = flag.String("trace", "", "Write execution trace to file")
	cpuProfile  = flag.String("cpuprofile", "", "Write CPU profile to file")
	memProfile  = flag.String("memprofile", "", "Write memory profile to file")
)

func main() {
	flag.Parse()

	// ── Profiling setup ──
	if *cpuProfile != "" {
		f, err := os.Create(*cpuProfile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "cpuprofile: %v\n", err)
			os.Exit(1)
		}
		defer f.Close()
		runtime.SetCPUProfileRate(500) // higher resolution
		if err := pprof.StartCPUProfile(f); err != nil {
			fmt.Fprintf(os.Stderr, "start cpu profile: %v\n", err)
			os.Exit(1)
		}
		defer pprof.StopCPUProfile()
		fmt.Fprintf(os.Stderr, "CPU profiling → %s\n", *cpuProfile)
	}

	if *traceFile != "" {
		f, err := os.Create(*traceFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "trace: %v\n", err)
			os.Exit(1)
		}
		defer f.Close()
		if err := trace.Start(f); err != nil {
			fmt.Fprintf(os.Stderr, "start trace: %v\n", err)
			os.Exit(1)
		}
		defer trace.Stop()
		fmt.Fprintf(os.Stderr, "Execution tracing → %s\n", *traceFile)
	}

	if *memProfile != "" {
		defer func() {
			f, err := os.Create(*memProfile)
			if err != nil {
				fmt.Fprintf(os.Stderr, "memprofile: %v\n", err)
				return
			}
			defer f.Close()
			runtime.GC()
			pprof.WriteHeapProfile(f)
			fmt.Fprintf(os.Stderr, "Memory profile → %s\n", *memProfile)
		}()
	}

	if *profileAddr != "" {
		go func() {
			fmt.Fprintf(os.Stderr, "pprof HTTP → http://%s/debug/pprof/\n", *profileAddr)
			http.ListenAndServe(*profileAddr, nil)
		}()
	}
	// ── End profiling setup ──

	// Load config
	cfg, err := loadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Config error: %v\n", err)
		os.Exit(1)
	}

	// Expand paths in config
	expandConfigPaths(cfg)

	// Doctor mode
	if *doctorMode {
		runDoctor(cfg)
		return
	}

	// Ensure .katty directory and default files
	if err := startup.EnsureDir(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
	}

	// Read startup files
	sFiles, sWarnings := startup.Load(cfg.Startup)
	for _, w := range sWarnings {
		fmt.Fprintf(os.Stderr, "Warning: %s\n", w)
	}

	// Probe environment
	env := envprobe.Probe(cfg.Environment)

	// Scan capabilities
	caps := scanCapabilities(cfg)

	// Initialize built-in tools
	registry := builtin.NewRegistry()

	// Set target registry
	builtin.SetTargetRegistry(cfg.Targets)

	// Build tool list for system context
	var builtinTools []systemctx.ToolDef
	for _, t := range registry.List() {
		builtinTools = append(builtinTools, systemctx.ToolDef{
			Name:        t.Name,
			Description: t.Description,
			Server:      "katty",
		})
	}

	// Create system context
	sysCtx := &systemctx.SystemContext{
		Env:          env,
		StartupFiles: sFiles,
		Capabilities: caps,
		Targets:      cfg.Targets,
		BuiltinTools: builtinTools,
	}

	// Print system context if requested
	if *printSystem {
		printSystemContext(sysCtx)
		return
	}

	// Initialize MCP manager
	var mcpMgr *mcp.Manager
	if !*noMCP {
		mcpMgr = mcp.NewManager(cfg.MCPServers)
		// Start MCP servers
		if err := mcpMgr.StartAll(context.Background()); err != nil {
			fmt.Fprintf(os.Stderr, "MCP startup warning: %v\n", err)
		}

		// Populate MCP info in system context
		for _, s := range mcpMgr.ListServers() {
			sysCtx.MCPServers = append(sysCtx.MCPServers, systemctx.MCPServerEntry{
				Name:  s.Name,
				State: string(s.State),
			})
		}
		for _, t := range mcpMgr.ListTools() {
			sysCtx.MCPTools = append(sysCtx.MCPTools, systemctx.ToolDef{
				Name:        t.Name,
				Description: t.Description,
				Server:      t.Server,
			})
		}
	}

	// Initialize DeepSeek client
	ds := deepseek.New(cfg.Model)

	// Create transcript logger
	ts, err := transcript.New(cfg.Transcripts.Dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Transcript warning: %v\n", err)
		ts = nil
	}
	if ts != nil {
		defer ts.Close()
	}

	// Start REPL
	r := repl.New(cfg, ds, registry, mcpMgr, sysCtx, ts)
	r.Run()

	// Cleanup
	if mcpMgr != nil {
		mcpMgr.StopAll(context.Background())
	}
}

func loadConfig(path string) (*config.Config, error) {
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("cannot determine home directory: %w", err)
		}
		path = filepath.Join(home, ".katty", "config.json")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) && path != "" {
			fmt.Fprintf(os.Stderr, "Config not found at %s, using defaults\n", path)
			cfg := config.DefaultConfig()
			return &cfg, nil
		}
		return nil, err
	}

	// Start from defaults so missing sections get sensible values
	cfg := config.DefaultConfig()
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	return &cfg, nil
}

func expandConfigPaths(cfg *config.Config) {
	home, _ := os.UserHomeDir()

	for i, f := range cfg.Startup.Files {
		cfg.Startup.Files[i] = expandPath(f, home)
	}

	for name, t := range cfg.Targets {
		if t.IdentityFile != "" {
			t.IdentityFile = expandPath(t.IdentityFile, home)
			cfg.Targets[name] = t
		}
	}

	if cfg.Transcripts.Dir != "" {
		cfg.Transcripts.Dir = expandPath(cfg.Transcripts.Dir, home)
	}

	for name, s := range cfg.MCPServers {
		if s.CWD != "" {
			s.CWD = expandPath(s.CWD, home)
			cfg.MCPServers[name] = s
		}
	}
}

func expandPath(path, home string) string {
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	return path
}

func scanCapabilities(cfg *config.Config) map[string][]string {
	caps := make(map[string][]string)
	if !cfg.Capabilities.ScanOnStartup {
		return caps
	}

	families := cfg.Capabilities.Families
	if len(families) == 0 {
		families = defaultFamilies()
	}

	for family, tools := range families {
		var found []string
		for _, tool := range tools {
			if _, err := exec.LookPath(tool); err == nil {
				found = append(found, tool)
			}
		}
		if len(found) > 0 {
			caps[family] = found
		}
	}

	return caps
}

func defaultFamilies() map[string][]string {
	return map[string][]string{
		"shells":    {"sh", "bash", "zsh", "ksh", "fish"},
		"compilers": {"cc", "gcc", "clang", "go", "rustc"},
		"build":     {"make", "cmake", "ninja", "meson"},
		"debuggers": {"gdb", "lldb"},
		"tracers":   {"strace", "truss", "ktrace", "dtrace", "perf", "bpftrace"},
		"elf_tools": {"readelf", "objdump", "nm", "addr2line"},
		"network":   {"ping", "nc", "curl", "wget", "ssh", "scp", "rsync"},
		"archive":   {"tar", "gzip", "xz", "zip", "unzip"},
		"text":      {"awk", "sed", "grep", "find", "xargs"},
		"docs":      {"man", "apropos", "info"},
		"system":    {"uname", "sysctl", "dmesg", "mount", "ps", "top"},
	}
}

func printSystemContext(sysCtx *systemctx.SystemContext) {
	fmt.Println(sysCtx.Prompt())
}

func runDoctor(cfg *config.Config) {
	checks := make(map[string]string)

	// Config
	checks["Config"] = "ok"

	// DeepSeek API key
	key := os.Getenv(cfg.Model.APIKeyEnv)
	if key != "" {
		checks["DeepSeek API key"] = fmt.Sprintf("ok (%s)", cfg.Model.APIKeyEnv)
	} else {
		checks["DeepSeek API key"] = fmt.Sprintf("missing (%s not set)", cfg.Model.APIKeyEnv)
	}

	// Startup files
	startupFiles, warnings := startup.Load(cfg.Startup)
	checks["Startup files"] = fmt.Sprintf("%d ok, %d warnings", len(startupFiles), len(warnings))

	// Environment probe
	env := envprobe.Probe(cfg.Environment)
	if env.Uname != "" {
		checks["Environment probe"] = "ok"
	} else {
		checks["Environment probe"] = "failed"
	}

	// Capabilities
	caps := scanCapabilities(cfg)
	checks["Capabilities"] = fmt.Sprintf("ok, %d families scanned", len(caps))

	// Built-in tools
	registry := builtin.NewRegistry()
	checks["Built-in tools"] = fmt.Sprintf("ok, %d tools", registry.Count())

	// proc.exec test
	ctx := context.Background()
	echoPath := "/bin/echo"
	if p, err := exec.LookPath("echo"); err == nil {
		echoPath = p
	}
	if t := registry.Get("katty.proc.exec"); t != nil {
		result := t.Handler(ctx, map[string]interface{}{
			"cmd": echoPath, "args": []interface{}{"hello"}, "timeout_seconds": 5,
		})
		if result.IsError {
			checks["katty.proc.exec"] = fmt.Sprintf("failed: %s", result.Error.Message)
		} else {
			checks["katty.proc.exec"] = "ok"
		}
	}

	// Cache directory
	home, _ := os.UserHomeDir()
	cacheDir := filepath.Join(home, ".katty")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		checks["Cache"] = fmt.Sprintf("error: %v", err)
	} else {
		checks["Cache"] = "ok (rw)"
	}

	// MCP servers
	for name, srv := range cfg.MCPServers {
		if srv.Enabled {
			checks[fmt.Sprintf("MCP %s", name)] = "enabled"
		} else {
			checks[fmt.Sprintf("MCP %s", name)] = "disabled"
		}
	}

	// Targets
	targetCount := 0
	localOK := false
	for name, t := range cfg.Targets {
		targetCount++
		if t.Type == "local" {
			localOK = true
		} else {
			checks[fmt.Sprintf("Target %s", name)] = fmt.Sprintf("ssh %s@%s:%d", t.User, t.Host, t.Port)
		}
	}
	if localOK {
		checks["Targets"] = fmt.Sprintf("local ok (%d total)", targetCount)
	} else {
		checks["Targets"] = fmt.Sprintf("%d configured, no local target", targetCount)
	}

	// Print results sorted
	keys := make([]string, 0, len(checks))
	for k := range checks {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	fmt.Println("Katty Doctor:")
	for _, k := range keys {
		fmt.Printf("  %s: %s\n", k, checks[k])
	}
}
