package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"tailfile.local/agent"
)

func main() {
	rest, action, system, err := extractServiceVerb(os.Args[1:])
	if err != nil {
		log.Fatal(err)
	}
	if system {
		log.Fatal(agent.ErrUserServiceOnly)
	}
	cfg, err := parseConfig(rest)
	if err != nil {
		log.Fatal(err)
	}
	if action != "" {
		msg, err := agent.ControlService(action, cfg, rest)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(msg)
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := agent.Run(ctx, cfg); err != nil {
		log.Fatal(err)
	}
}

func parseConfig(args []string) (agent.Config, error) {
	cfg := agent.Config{}
	var read, write string
	fs := flag.NewFlagSet("tailfile-agent", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.StringVar(&cfg.Hostname, "hostname", "", "tsnet node hostname (default tailfile-<host>)")
	fs.StringVar(&cfg.StateDir, "state", "", "state directory (default under the user config dir)")
	fs.IntVar(&cfg.Port, "port", 8644, "tailnet HTTP port")
	fs.StringVar(&cfg.LocalAddr, "listen", "127.0.0.1:8645", "localhost API for the GUI")
	fs.StringVar(&cfg.Owner, "owner", "", "tailnet login allowed to call this agent (default: this node's own user)")
	fs.StringVar(&read, "read", "", "comma-separated readable roots (default: ~/Tailfile)")
	fs.StringVar(&write, "write", "", "comma-separated writable roots (default: none, read-only)")
	fs.StringVar(&cfg.Net, "net", "auto", "tailnet transport: auto, system, or tsnet")
	fs.BoolVar(&cfg.Demo, "demo", false, "in-process demo nodes, no tailnet")
	fs.StringVar(&cfg.AuthKey, "authkey", "", "tsnet auth key (prefer TS_AUTHKEY or the settings file; never commit it)")
	fs.IntVar(&cfg.ChunkSize, "chunk", 0, "upload chunk size in bytes")
	fs.BoolVar(&cfg.Ephemeral, "ephemeral", false, "register an ephemeral tsnet node")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: tailfile-agent [flags]\n")
		fmt.Fprintf(fs.Output(), "       tailfile-agent service install|uninstall|start|stop|status [flags]\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return agent.Config{}, err
	}
	cfg.ReadRoots = splitList(read)
	cfg.WriteRoots = splitList(write)
	if cfg.AuthKey != "" {
		fmt.Fprintln(os.Stderr, "tailfile: -authkey was set; prefer TS_AUTHKEY or the settings screen so the key stays out of shell history and the service command")
	}
	return cfg, nil
}

// extractServiceVerb pulls `service <verb>` out of the argument list.
// Flags that take a value are skipped so a path named "service" is not a verb.
// --system is removed and reported; it is not a supported install scope.
func extractServiceVerb(args []string) (rest []string, action string, system bool, err error) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			rest = append(rest, args[i:]...)
			break
		}
		if a == "--system" || a == "-system" {
			system = true
			continue
		}
		if takesValue(a) && !strings.Contains(a, "=") {
			rest = append(rest, a)
			if i+1 < len(args) {
				i++
				rest = append(rest, args[i])
			}
			continue
		}
		if a == "service" {
			if action != "" {
				return nil, "", false, fmt.Errorf("service specified twice")
			}
			if i+1 >= len(args) {
				return nil, "", false, fmt.Errorf("usage: tailfile-agent service install|uninstall|start|stop|status")
			}
			action = args[i+1]
			switch action {
			case "install", "uninstall", "start", "stop", "status":
			default:
				return nil, "", false, fmt.Errorf("unknown service command %q", action)
			}
			i++
			continue
		}
		rest = append(rest, a)
	}
	return rest, action, system, nil
}

func takesValue(a string) bool {
	switch a {
	case "-hostname", "--hostname", "-state", "--state", "-port", "--port",
		"-listen", "--listen", "-owner", "--owner", "-read", "--read",
		"-write", "--write", "-net", "--net", "-authkey", "--authkey",
		"-chunk", "--chunk":
		return true
	default:
		return false
	}
}

func splitList(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
