//go:generate go run ./scripts/gen-winres

package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"tailfile.local/internal/agent"
	"tailfile.local/internal/cliflag"
	"tailfile.local/internal/gui"
)

type cliMode int

const (
	modeGUI cliMode = iota
	modeAgent
	modeService
	modeVersion
	modeHelp
)

type parsedCLI struct {
	mode         cliMode
	agentCfg     agent.Config
	demo         bool
	agentURL     string
	explicitAddr bool
	attachOnly   bool
	tokenFile    string
	svcAction    string
	svcFlags     []string
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	os.Exit(run(os.Args[1:], nil, nil))
}

func resolveStreams(stdout, stderr io.Writer) (io.Writer, io.Writer) {
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}
	return stdout, stderr
}

func run(args []string, stdout, stderr io.Writer) int {
	parseErrOut := stderr
	var errBuf bytes.Buffer
	if parseErrOut == nil {
		parseErrOut = &errBuf
	}
	p, err := parseCLI(args, parseErrOut)
	if err != nil {
		ensureConsole()
		_, stderr = resolveStreams(stdout, stderr)
		if errBuf.Len() > 0 {
			_, _ = errBuf.WriteTo(stderr)
		}
		fmt.Fprintln(stderr, "tailfile:", err)
		return 2
	}

	switch p.mode {
	case modeVersion:
		ensureConsole()
		stdout, _ = resolveStreams(stdout, stderr)
		fmt.Fprintf(stdout, "tailfile %s\n", agent.Version)
		return 0

	case modeHelp:
		ensureConsole()
		stdout, _ = resolveStreams(stdout, stderr)
		printUsage(stdout)
		return 0

	case modeService:
		ensureConsole()
		stdout, stderr = resolveStreams(stdout, stderr)
		msg, err := agent.ControlService(p.svcAction, p.agentCfg, p.svcFlags)
		if err != nil {
			fmt.Fprintln(stderr, "tailfile:", err)
			return 1
		}
		fmt.Fprintln(stdout, msg)
		return 0

	case modeAgent:
		ensureConsole()
		_, stderr = resolveStreams(stdout, stderr)
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if err := agent.Run(ctx, p.agentCfg); err != nil {
			fmt.Fprintln(stderr, "tailfile:", err)
			return 1
		}
		return 0

	case modeGUI:
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		conn, err := agent.Connect(ctx, agent.ConnectConfig{
			Agent:        p.agentCfg,
			LocalURL:     p.agentURL,
			ExplicitAddr: p.explicitAddr,
			TokenFile:    p.tokenFile,
			AttachOnly:   p.attachOnly,
		})
		if err != nil {
			ensureConsole()
			_, stderr = resolveStreams(stdout, stderr)
			fmt.Fprintln(stderr, "tailfile:", err)
			return 1
		}
		defer conn.Close()
		singleInstance := !p.demo && !p.explicitAddr && !p.attachOnly
		return gui.Run(conn, gui.GUIOptions{TokenFile: p.tokenFile}, singleInstance)
	}
	return 0
}

func parseCLI(args []string, errOut io.Writer) (parsedCLI, error) {
	if len(args) > 0 {
		switch args[0] {
		case "version", "--version", "-version", "-v":
			return parsedCLI{mode: modeVersion}, nil
		case "help", "--help", "-help", "-h":
			return parsedCLI{mode: modeHelp}, nil
		case "agent":
			cfg, _, _, _, _, err := parseAgentFlags("tailfile agent", args[1:], false, errOut)
			return parsedCLI{mode: modeAgent, agentCfg: cfg}, err
		case "service":
			return parseServiceCLI(args[1:], errOut)
		}
	}

	// Default GUI invocation also checks for top-level --help / --version via flag errors.
	cfg, agentURL, tokenFile, attachOnly, explicitAddr, err := parseAgentFlags("tailfile", args, true, errOut)
	if err != nil {
		if err == flag.ErrHelp {
			return parsedCLI{mode: modeHelp}, nil
		}
		return parsedCLI{}, err
	}
	return parsedCLI{
		mode:         modeGUI,
		agentCfg:     cfg,
		demo:         cfg.Demo,
		agentURL:     agentURL,
		explicitAddr: explicitAddr,
		attachOnly:   attachOnly,
		tokenFile:    tokenFile,
	}, nil
}

func parseServiceCLI(args []string, errOut io.Writer) (parsedCLI, error) {
	if len(args) == 0 {
		return parsedCLI{}, fmt.Errorf("usage: tailfile service <install|uninstall|start|stop|status> [agent flags]")
	}
	if args[0] == "--help" || args[0] == "-help" || args[0] == "-h" || args[0] == "help" {
		return parsedCLI{mode: modeHelp}, nil
	}
	action := args[0]
	switch action {
	case "install", "uninstall", "start", "stop", "status":
	default:
		return parsedCLI{}, fmt.Errorf("unknown service command %q (want install, uninstall, start, stop, or status)", action)
	}
	rawFlags := args[1:]
	prepArgs, _, err := agent.PrepareServiceArgs(rawFlags)
	if err != nil {
		return parsedCLI{}, err
	}
	cfg, _, _, _, _, err := parseAgentFlags("tailfile service "+action, prepArgs, false, errOut)
	if err != nil {
		if err == flag.ErrHelp {
			return parsedCLI{mode: modeHelp}, nil
		}
		return parsedCLI{}, err
	}
	return parsedCLI{
		mode:      modeService,
		agentCfg:  cfg,
		svcAction: action,
		svcFlags:  rawFlags,
	}, nil
}

func registerAgentFlags(fs *flag.FlagSet, cfg *agent.Config, readRaw, writeRaw *string) {
	fs.StringVar(&cfg.Hostname, "hostname", "", "Tailscale device hostname")
	fs.StringVar(&cfg.StateDir, "state", "", "State directory")
	fs.StringVar(&cfg.Net, "net", "system", "Tailnet mode: system (default) or tsnet (experimental)")
	fs.IntVar(&cfg.Port, "port", 8644, "Tailnet listen port")
	fs.StringVar(&cfg.LocalAddr, "local", "", "Optional loopback GUI/API listen address (e.g. 127.0.0.1:8645)")
	fs.StringVar(&cfg.Owner, "owner", "", "Owner login (or TAILFILE_OWNER)")
	fs.StringVar(readRaw, "read", "", "Comma-separated shared read roots")
	fs.StringVar(writeRaw, "write", "", "Comma-separated writable roots")
	fs.BoolVar(&cfg.ReadOnly, "read-only", false, "Disable writes on shared roots")
	fs.BoolVar(&cfg.Demo, "demo", false, "Run isolated 5-node in-memory demo tailnet")
	fs.StringVar(&cfg.AuthKey, "authkey", "", "Tailscale auth key for --net tsnet (experimental, or TS_AUTHKEY)")
	fs.IntVar(&cfg.ChunkSize, "chunk", 1<<20, "Upload chunk size in bytes")
	fs.BoolVar(&cfg.Ephemeral, "ephemeral", false, "Register as an ephemeral node in --net tsnet (experimental)")
}

func parseAgentFlags(name string, args []string, guiMode bool, errOut io.Writer) (cfg agent.Config, agentURL, tokenFile string, attachOnly, explicitAddr bool, err error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	if errOut != nil {
		fs.SetOutput(errOut)
	}
	fs.Usage = cliflag.Usage(fs, "Usage: "+name+" [flags]")

	var readRaw, writeRaw string
	registerAgentFlags(fs, &cfg, &readRaw, &writeRaw)

	if guiMode {
		fs.StringVar(&agentURL, "agent", "", "Connect to an external agent URL (or TAILFILE_AGENT)")
		fs.BoolVar(&attachOnly, "attach-only", false, "Attach to running local agent only; do not embed")
		fs.StringVar(&tokenFile, "token-file", "", "Path to gui-token file")
	}

	if err := fs.Parse(args); err != nil {
		return agent.Config{}, "", "", false, false, err
	}
	if fs.NArg() > 0 {
		return agent.Config{}, "", "", false, false, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}

	cfg.ReadRoots = splitRoots(readRaw)
	cfg.WriteRoots = splitRoots(writeRaw)
	if cfg.ReadOnly {
		cfg.WriteRoots = nil
	}

	if guiMode {
		if strings.TrimSpace(agentURL) != "" {
			explicitAddr = true
		} else if envURL := strings.TrimSpace(os.Getenv("TAILFILE_AGENT")); envURL != "" {
			agentURL = envURL
			explicitAddr = true
		}
	}
	return cfg, agentURL, tokenFile, attachOnly, explicitAddr, nil
}

func splitRoots(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		p := strings.TrimSpace(part)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "Tailfile — unified tailnet file browser & agent")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  tailfile [flags]                        Launch GUI (auto-attaches or embeds agent)")
	fmt.Fprintln(w, "  tailfile --demo                         Launch GUI with isolated 5-node demo cluster")
	fmt.Fprintln(w, "  tailfile agent [flags]                  Run headless background agent")
	fmt.Fprintln(w, "  tailfile service <verb> [flags]         Manage per-user background service (install|uninstall|start|stop|status)")
	fmt.Fprintln(w, "  tailfile version                        Print version")
	fmt.Fprintln(w, "  tailfile help                           Show this help")
	fmt.Fprintln(w)
	fs := flag.NewFlagSet("tailfile", flag.ContinueOnError)
	fs.SetOutput(w)
	var cfg agent.Config
	var readRaw, writeRaw, agentURL, tokenFile string
	var attachOnly bool
	registerAgentFlags(fs, &cfg, &readRaw, &writeRaw)
	fs.StringVar(&agentURL, "agent", "", "External agent URL (GUI mode)")
	fs.BoolVar(&attachOnly, "attach-only", false, "Attach only; never start embedded agent (GUI mode)")
	fs.StringVar(&tokenFile, "token-file", "", "Path to gui-token file (GUI mode)")
	fmt.Fprintln(w, "Flags:")
	cliflag.PrintDefaults(fs)
}
