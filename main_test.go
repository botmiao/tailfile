package main

import (
	"bytes"
	"strings"
	"testing"

	"tailfile.local/internal/agent"
)

func TestParseCLIModes(t *testing.T) {
	p, err := parseCLI(nil, nil)
	if err != nil || p.mode != modeGUI || p.agentCfg.Net != "system" {
		t.Fatalf("empty args: got %+v err=%v", p, err)
	}

	p, err = parseCLI([]string{"--demo", "--read-only"}, nil)
	if err != nil || p.mode != modeGUI || !p.demo || !p.agentCfg.ReadOnly {
		t.Fatalf("demo gui args: got %+v err=%v", p, err)
	}

	p, err = parseCLI([]string{"--agent", "http://127.0.0.1:8700", "--token-file", "/tmp/tok"}, nil)
	if err != nil || p.mode != modeGUI || !p.explicitAddr || p.agentURL != "http://127.0.0.1:8700" || p.tokenFile != "/tmp/tok" {
		t.Fatalf("explicit agent args: got %+v err=%v", p, err)
	}

	p, err = parseCLI([]string{"agent"}, nil)
	if err != nil || p.mode != modeAgent || p.agentCfg.Net != "system" || p.agentCfg.LocalAddr != "" {
		t.Fatalf("default agent args: got %+v err=%v", p, err)
	}

	p, err = parseCLI([]string{"agent", "--local", "127.0.0.1:8645"}, nil)
	if err != nil || p.mode != modeAgent || p.agentCfg.LocalAddr != "127.0.0.1:8645" {
		t.Fatalf("explicit local addr: got %+v err=%v", p, err)
	}

	p, err = parseCLI([]string{"agent", "--net", "system", "--read", "/a,/b", "--read-only"}, nil)
	if err != nil || p.mode != modeAgent || p.agentCfg.Net != "system" || len(p.agentCfg.ReadRoots) != 2 || !p.agentCfg.ReadOnly {
		t.Fatalf("agent subcommand: got %+v err=%v", p, err)
	}

	p, err = parseCLI([]string{"agent", "--net", "tsnet"}, nil)
	if err != nil || p.mode != modeAgent || p.agentCfg.Net != "tsnet" {
		t.Fatalf("explicit tsnet flag: got %+v err=%v", p, err)
	}

	p, err = parseCLI([]string{"service", "install", "--net", "tsnet", "--write", "/srv/data"}, nil)
	if err != nil || p.mode != modeService || p.svcAction != "install" || len(p.agentCfg.WriteRoots) != 1 {
		t.Fatalf("service subcommand: got %+v err=%v", p, err)
	}

	if _, err := parseCLI([]string{"service", "install", "--system"}, nil); err == nil {
		t.Fatal("expected --system rejection on service install")
	}

	for _, arg := range []string{"version", "--version", "-v"} {
		p, err = parseCLI([]string{arg}, nil)
		if err != nil || p.mode != modeVersion {
			t.Fatalf("%s: got %+v err=%v", arg, p, err)
		}
	}
	for _, arg := range []string{"help", "--help", "-h"} {
		p, err = parseCLI([]string{arg}, nil)
		if err != nil || p.mode != modeHelp {
			t.Fatalf("%s: got %+v err=%v", arg, p, err)
		}
	}
}

func TestVersionAndHelpOutput(t *testing.T) {
	var out, errBuf bytes.Buffer
	code := run([]string{"--version"}, &out, &errBuf)
	if code != 0 || !strings.Contains(out.String(), agent.Version) {
		t.Fatalf("version code=%d out=%q err=%q", code, out.String(), errBuf.String())
	}

	out.Reset()
	errBuf.Reset()
	code = run([]string{"--help"}, &out, &errBuf)
	if code != 0 || !strings.Contains(out.String(), "tailfile service") || !strings.Contains(out.String(), "tailfile agent") || !strings.Contains(out.String(), "tsnet (experimental)") {
		t.Fatalf("help code=%d out=%q err=%q", code, out.String(), errBuf.String())
	}
}
