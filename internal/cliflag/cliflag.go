// Package cliflag prints flag help in the --name form used by tailfile docs.
//
// Go's flag package accepts both -name and --name. Tailfile documents --name;
// -name keeps working for existing scripts and installed services.
package cliflag

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

// PrintDefaults is fs.PrintDefaults with every flag shown as --name.
func PrintDefaults(fs *flag.FlagSet) {
	var buf strings.Builder
	out := fs.Output()
	fs.SetOutput(&buf)
	fs.PrintDefaults()
	fs.SetOutput(out)
	for _, line := range strings.SplitAfter(buf.String(), "\n") {
		if strings.HasPrefix(line, "  -") {
			line = "  --" + line[len("  -"):]
		}
		fmt.Fprint(out, line)
	}
}

// Usage returns a flag.Usage function that prints the given header lines,
// a blank line, and the flags in --name form.
func Usage(fs *flag.FlagSet, header ...string) func() {
	return func() {
		for _, h := range header {
			fmt.Fprintln(fs.Output(), h)
		}
		fmt.Fprintln(fs.Output())
		PrintDefaults(fs)
	}
}

// UseDoubleDash sets flag.CommandLine's usage to the --name form.
func UseDoubleDash() {
	flag.CommandLine.Usage = Usage(flag.CommandLine, "Usage: "+progName()+" [flags]")
}

func progName() string {
	if len(os.Args) == 0 {
		return "tailfile"
	}
	name := os.Args[0]
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	return name
}
