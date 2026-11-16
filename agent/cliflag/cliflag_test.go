package cliflag

import (
	"flag"
	"strings"
	"testing"
)

func TestPrintDefaultsUsesDoubleDash(t *testing.T) {
	fs := flag.NewFlagSet("x", flag.ContinueOnError)
	fs.String("read", "", "readable roots")
	fs.Bool("demo", false, "demo mode")
	var buf strings.Builder
	fs.SetOutput(&buf)
	PrintDefaults(fs)
	got := buf.String()
	for _, want := range []string{"  --read string\n", "  --demo\n", "readable roots"} {
		if !strings.Contains(got, want) {
			t.Fatalf("help missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "\n  -read") || strings.HasPrefix(got, "  -read") {
		t.Fatalf("help still shows single-dash flag:\n%s", got)
	}
}

func TestSingleAndDoubleDashBothParse(t *testing.T) {
	for _, args := range [][]string{{"-read", "/a"}, {"--read", "/a"}, {"--read=/a"}} {
		fs := flag.NewFlagSet("x", flag.ContinueOnError)
		read := fs.String("read", "", "")
		if err := fs.Parse(args); err != nil || *read != "/a" {
			t.Fatalf("%v: read=%q err=%v", args, *read, err)
		}
	}
}
