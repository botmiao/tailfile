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
	cfg := agent.Config{}
	var read, write string
	flag.StringVar(&cfg.Hostname, "hostname", "", "tsnet node hostname (default tailfile-<host>)")
	flag.StringVar(&cfg.StateDir, "state", "", "tsnet state directory (default under the user config dir)")
	flag.IntVar(&cfg.Port, "port", 8644, "tailnet HTTP port")
	flag.StringVar(&cfg.LocalAddr, "listen", "127.0.0.1:8645", "localhost API for the GUI")
	flag.StringVar(&cfg.Owner, "owner", "", "tailnet login allowed to call this agent (default: this node's own user)")
	flag.StringVar(&read, "read", "", "comma-separated readable roots (default: ~/Tailfile)")
	flag.StringVar(&write, "write", "", "comma-separated writable roots (default: none, read-only)")
	flag.StringVar(&cfg.Net, "net", "auto", "tailnet transport: auto, system, or tsnet")
	flag.BoolVar(&cfg.Demo, "demo", false, "in-process demo nodes, no tailnet")
	flag.StringVar(&cfg.AuthKey, "authkey", "", "tsnet auth key (prefer TS_AUTHKEY; never commit it)")
	flag.IntVar(&cfg.ChunkSize, "chunk", 0, "upload chunk size in bytes")
	flag.BoolVar(&cfg.Ephemeral, "ephemeral", false, "register an ephemeral tsnet node")
	flag.Parse()

	cfg.ReadRoots = splitList(read)
	cfg.WriteRoots = splitList(write)
	if cfg.AuthKey != "" {
		fmt.Fprintln(os.Stderr, "tailfile: -authkey was set; prefer the TS_AUTHKEY environment variable so the key stays out of shell history")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := agent.Run(ctx, cfg); err != nil {
		log.Fatal(err)
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
