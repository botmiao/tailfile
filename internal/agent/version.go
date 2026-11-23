package agent

// Version is reported by /v1/health, /v1/info, and tailfile --version.
// Release builds override it via -ldflags "-X tailfile.local/internal/agent.Version=...".
var Version = "0.2.0"
