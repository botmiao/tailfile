// Package guitoken finds the per-start localhost bearer without linking tsnet.
// The agent writes the file. Both GUIs read it. The value is never logged.
package guitoken

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FileName is the token file inside the agent state directory. Mode 0600.
const FileName = "gui-token"

// DefaultHostname matches the agent's default node name.
func DefaultHostname() string {
	h, err := os.Hostname()
	if err != nil || strings.TrimSpace(h) == "" {
		h = "node"
	}
	return "tailfile-" + sanitize(h)
}

// DefaultStateDir is UserConfigDir/tailfile/<hostname>, the same place the agent uses.
func DefaultStateDir(hostname string) string {
	if hostname == "" {
		hostname = DefaultHostname()
	}
	base, err := os.UserConfigDir()
	if err != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, "tailfile", hostname)
}

// Path is the token file. tokenFile wins. Otherwise <stateDir>/gui-token,
// and an empty stateDir uses DefaultStateDir("").
func Path(tokenFile, stateDir string) string {
	if tokenFile != "" {
		return tokenFile
	}
	if stateDir == "" {
		stateDir = DefaultStateDir("")
	}
	return filepath.Join(stateDir, FileName)
}

// Load reads the bearer. Errors name the path and never the token.
func Load(tokenFile, stateDir string) (string, error) {
	path := Path(tokenFile, stateDir)
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read GUI token from %s: %w", path, err)
	}
	tok := strings.TrimSpace(string(raw))
	if tok == "" {
		return "", fmt.Errorf("GUI token file %s is empty", path)
	}
	return tok, nil
}

func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := b.String()
	if out == "" || out == "." || out == ".." {
		return "node"
	}
	return out
}
