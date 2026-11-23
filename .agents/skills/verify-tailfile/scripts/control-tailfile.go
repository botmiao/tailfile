package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image/png"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

type runSession struct {
	RunID           string `json:"runId"`
	Mode            string `json:"mode"`
	RepoRoot        string `json:"repoRoot"`
	BinPath         string `json:"binPath"`
	RunDir          string `json:"runDir"`
	EvidenceDir     string `json:"evidenceDir"`
	LauncherPID     int    `json:"launcherPid,omitempty"`
	AgentPID        int    `json:"agentPid,omitempty"`
	GUIPID          int    `json:"guiPid"`
	AutomationAddr  string `json:"automationAddr"`
	APIURL          string `json:"apiUrl,omitempty"`
	TokenFile       string `json:"tokenFile,omitempty"`
	AgentStateDir   string `json:"agentStateDir,omitempty"`
	GUIStateDir     string `json:"guiStateDir"`
	ExpectedVersion string `json:"expectedVersion"`
	StartedAt       string `json:"startedAt"`
}

type rect struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type windowInfo struct {
	ID            int    `json:"id"`
	Title         string `json:"title"`
	Kind          string `json:"kind"`
	Bounds        rect   `json:"bounds"`
	ContentBounds rect   `json:"contentBounds"`
	Focused       bool   `json:"focused"`
	Visible       bool   `json:"visible"`
}

type treeNode struct {
	ID          string   `json:"id"`
	Parent      string   `json:"parent,omitempty"`
	Role        string   `json:"role"`
	Key         any      `json:"key,omitempty"`
	Label       string   `json:"label,omitempty"`
	Value       string   `json:"value,omitempty"`
	Placeholder string   `json:"placeholder,omitempty"`
	Description string   `json:"description,omitempty"`
	Bounds      rect     `json:"bounds"`
	States      []string `json:"states,omitempty"`
	Actions     []string `json:"actions,omitempty"`
}

func (n treeNode) keyString() string {
	if n.Key == nil {
		return ""
	}
	return fmt.Sprint(n.Key)
}

func (n treeNode) center() (float64, float64) {
	return n.Bounds.X + n.Bounds.Width/2, n.Bounds.Y + n.Bounds.Height/2
}

func (n treeNode) hasState(target string) bool {
	for _, s := range n.States {
		if s == target {
			return true
		}
	}
	return false
}

func (n treeNode) summary() string {
	var parts []string
	parts = append(parts, fmt.Sprintf("[%s]", n.Role))
	if n.Label != "" {
		parts = append(parts, fmt.Sprintf("label=%q", n.Label))
	}
	if ks := n.keyString(); ks != "" {
		parts = append(parts, fmt.Sprintf("key=%q", ks))
	}
	if n.Value != "" {
		parts = append(parts, fmt.Sprintf("value=%q", n.Value))
	}
	if n.Placeholder != "" && n.Value == "" {
		parts = append(parts, fmt.Sprintf("placeholder=%q", n.Placeholder))
	}
	if len(n.States) > 0 {
		parts = append(parts, "states=["+strings.Join(n.States, ",")+"]")
	}
	if len(n.Actions) > 0 {
		parts = append(parts, "actions=["+strings.Join(n.Actions, ",")+"]")
	}
	parts = append(parts, fmt.Sprintf("bounds=(%.0f,%.0f,%.0f,%.0f)", n.Bounds.X, n.Bounds.Y, n.Bounds.Width, n.Bounds.Height))
	return strings.Join(parts, " ")
}

type autoClient struct {
	addr   string
	nextID atomic.Int64
}

func newAutoClient(addr string) *autoClient {
	return &autoClient{addr: addr}
}

func (c *autoClient) call(method string, params any, out any) error {
	conn, err := net.DialTimeout("tcp", c.addr, 5*time.Second)
	if err != nil {
		return fmt.Errorf("dial automation %s: %w", c.addr, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))

	id := c.nextID.Add(1)
	req := map[string]any{"id": id, "method": method}
	if params != nil {
		req["params"] = params
	}
	line, err := json.Marshal(req)
	if err != nil {
		return err
	}
	if _, err := conn.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("write %s: %w", method, err)
	}

	r := bufio.NewReaderSize(conn, 256*1024)
	respLine, err := r.ReadBytes('\n')
	if err != nil {
		return fmt.Errorf("read %s response: %w", method, err)
	}

	var resp struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(respLine, &resp); err != nil {
		return fmt.Errorf("decode %s response: %w", method, err)
	}
	if resp.Error != nil {
		return &rpcError{Code: resp.Error.Code, Message: resp.Error.Message, Method: method}
	}
	if out != nil && len(resp.Result) > 0 {
		if err := json.Unmarshal(resp.Result, out); err != nil {
			return fmt.Errorf("decode %s result: %w", method, err)
		}
	}
	return nil
}

type rpcError struct {
	Method  string
	Code    string
	Message string
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("automation %s (%s): %s", e.Method, e.Code, e.Message)
}

func (c *autoClient) windows() ([]windowInfo, error) {
	var res struct {
		Windows []windowInfo `json:"windows"`
	}
	if err := c.call("windows", nil, &res); err != nil {
		return nil, err
	}
	return res.Windows, nil
}

func (c *autoClient) tree(winID int) ([]treeNode, error) {
	params := map[string]any{}
	if winID > 0 {
		params["window"] = winID
	}
	var res struct {
		Window int        `json:"window"`
		Nodes  []treeNode `json:"nodes"`
	}
	if err := c.call("tree", params, &res); err != nil {
		return nil, err
	}
	return res.Nodes, nil
}

func (c *autoClient) waitIdle(winID int, timeoutMs int) error {
	params := map[string]any{"timeout": timeoutMs}
	if winID > 0 {
		params["window"] = winID
	}
	err := c.call("idle", params, nil)
	var re *rpcError
	if errors.As(err, &re) && re.Code == "timeout" {
		// Tailfile schedules periodic 1s View polling timers or active animations;
		// treat idle timeout as best-effort settle rather than hard failure.
		return nil
	}
	return err
}

func (c *autoClient) mainWindowID() (int, windowInfo, error) {
	wins, err := c.windows()
	if err != nil {
		return 0, windowInfo{}, err
	}
	for _, w := range wins {
		if w.Title == "Tailfile" && w.Kind == "content" {
			return w.ID, w, nil
		}
	}
	if len(wins) > 0 {
		return wins[0].ID, wins[0], nil
	}
	return 0, windowInfo{}, errors.New("no automation windows found")
}

var validRunID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(2)
	}

	sub := os.Args[1]
	args := os.Args[2:]
	var exitCode int
	var err error

	switch sub {
	case "launch":
		err = cmdLaunch(args)
	case "wait-ready":
		err = cmdWaitReady(args)
	case "doctor":
		err = cmdDoctor(args)
	case "snapshot":
		err = cmdSnapshot(args)
	case "act":
		err = cmdAct(args)
	case "click":
		err = cmdClick(args)
	case "drag":
		err = cmdDrag(args)
	case "type":
		err = cmdType(args)
	case "key":
		err = cmdKey(args)
	case "wait":
		err = cmdWait(args)
	case "screenshot":
		err = cmdScreenshot(args)
	case "api":
		err = cmdAPI(args)
	case "cli":
		exitCode, err = cmdCLI(args)
	case "cleanup":
		err = cmdCleanup(args)
	case "help", "--help", "-h":
		printUsage()
		return
	default:
		err = fmt.Errorf("unknown subcommand %q", sub)
		exitCode = 2
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "control-tailfile %s: %v\n", sub, err)
		if exitCode == 0 {
			exitCode = 1
		}
		os.Exit(exitCode)
	}
	if exitCode != 0 {
		os.Exit(exitCode)
	}
}

func printUsage() {
	fmt.Print(`control-tailfile — drive isolated Tailfile instances for verification

Subcommands:
  launch      Build, start isolated Tailfile daemon/GUI, print READY, supervise children
  wait-ready  Wait until a background launch writes session.json (or launch.error) and passes health checks
  doctor      Read-only check: PIDs, build version, isolation, HTTP health, live window
  snapshot    Dump ARIA accessibility tree as compact lines (and optional JSON)
  act         Invoke accessibility action on an element (--key/--label/--role)
  click       Click center of matched element (--key/--key-suffix/--label/--role)
  drag        Pointer drag between coordinates (--from-x/y --to-x/y [--hold] [--release-only])
  type        Send text input events to focused element
  key         Send keyboard shortcut / accelerator event (e.g. Enter, F2, CmdOrCtrl+T)
  wait        Poll tree until substring/key appears or disappears
  screenshot  Capture real window PNG and validate non-blank decoded output
  api         Call local agent HTTP API with session bearer token (attached mode)
  cli         Run built verification binary for CLI subcommands and capture output
  cleanup     Kill ONLY tracked run PIDs and delete runDir, preserving evidenceDir
`)
}

func runDirFor(runID string) (string, error) {
	if !validRunID.MatchString(runID) {
		return "", fmt.Errorf("invalid run id %q (use [a-zA-Z0-9._-])", runID)
	}
	return filepath.Join(os.TempDir(), "tailfile-verify-runs", runID), nil
}

func sessionPathFor(runID string) (string, error) {
	dir, err := runDirFor(runID)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "session.json"), nil
}

func loadSession(runID string) (runSession, error) {
	if runID == "" {
		return runSession{}, errors.New("--run is required")
	}
	p, err := sessionPathFor(runID)
	if err != nil {
		return runSession{}, err
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		return runSession{}, fmt.Errorf("read session %s: %w", p, err)
	}
	var sess runSession
	if err := json.Unmarshal(raw, &sess); err != nil {
		return runSession{}, fmt.Errorf("parse session %s: %w", p, err)
	}
	return sess, nil
}

func findRepoRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	cur := wd
	for {
		gomod := filepath.Join(cur, "go.mod")
		if raw, err := os.ReadFile(gomod); err == nil && strings.Contains(string(raw), "module tailfile.local") {
			return cur, nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", fmt.Errorf("could not locate tailfile go.mod from %s", wd)
		}
		cur = parent
	}
}

func freePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

func withEnv(base []string, overrides map[string]string) []string {
	out := make([]string, 0, len(base)+len(overrides))
	seen := map[string]bool{}
	for _, entry := range base {
		k, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		matched := false
		for ovK := range overrides {
			if strings.EqualFold(k, ovK) {
				matched = true
				break
			}
		}
		if matched {
			continue
		}
		out = append(out, entry)
	}
	for k, v := range overrides {
		if !seen[k] {
			out = append(out, k+"="+v)
			seen[k] = true
		}
	}
	return out
}

func cmdLaunch(args []string) error {
	return runLaunch(args)
}

func runLaunch(args []string) (retErr error) {
	fs := flag.NewFlagSet("launch", flag.ContinueOnError)
	runID := fs.String("run", "run-"+time.Now().Format("20060102-150405"), "Unique verification run ID")
	mode := fs.String("mode", "attached", "Launch mode: attached or demo")
	evidenceFlag := fs.String("evidence", "", "Evidence directory (never deleted by cleanup)")
	repoFlag := fs.String("repo", "", "Tailfile repo root (auto-detected if omitted)")
	timeout := fs.Duration("timeout", 35*time.Second, "Readiness timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *mode != "attached" && *mode != "demo" {
		return fmt.Errorf("--mode must be 'attached' or 'demo', got %q", *mode)
	}
	runDir, err := runDirFor(*runID)
	if err != nil {
		return err
	}
	sessFile := filepath.Join(runDir, "session.json")
	launchErrFile := filepath.Join(runDir, "launch.error")
	if _, err := os.Stat(sessFile); err == nil {
		return fmt.Errorf("run %q already exists at %s; run cleanup first or choose a new --run ID", *runID, sessFile)
	}
	_ = os.Remove(launchErrFile)

	repoRoot := *repoFlag
	if repoRoot == "" {
		repoRoot, err = findRepoRoot()
		if err != nil {
			return err
		}
	}
	evidenceDir := *evidenceFlag
	if evidenceDir == "" {
		evidenceDir = filepath.Join(repoRoot, ".verify-proof", *runID)
	}
	absEvidence, err := filepath.Abs(evidenceDir)
	if err != nil {
		return err
	}
	evidenceDir = absEvidence

	binDir := filepath.Join(runDir, "bin")
	agentStateDir := filepath.Join(runDir, "agent-state")
	guiStateDir := filepath.Join(runDir, "gui-state")
	for _, d := range []string{binDir, agentStateDir, guiStateDir, evidenceDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}

	var startedPIDs []int
	rollback := true
	defer func() {
		if rollback {
			for i := len(startedPIDs) - 1; i >= 0; i-- {
				_ = killPID(startedPIDs[i])
			}
			_ = os.Remove(sessFile)
			if retErr != nil {
				// Leave launch.error intact so wait-ready fails fast instead of polling until timeout.
				_ = os.WriteFile(launchErrFile, []byte(retErr.Error()+"\n"), 0o644)
			}
		}
	}()

	// Pre-seed isolated GUI config so LoadPersist never falls back to user legacy nsh config.
	initialUI := `{
  "theme": "dark",
  "view": "list",
  "details": true,
  "sidebarCollapsed": false,
  "tabs": [],
  "activeTab": 0,
  "sortKey": "name",
  "sortAsc": true
}
`
	if err := os.WriteFile(filepath.Join(guiStateDir, "ui.json"), []byte(initialUI), 0o600); err != nil {
		return err
	}

	binName := "tailfile-verify"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	binPath := filepath.Join(binDir, binName)

	buildCmd := exec.Command("go", "build", "-tags", "e2e mygo_nowebview", "-o", binPath, ".")
	buildCmd.Dir = repoRoot
	buildCmd.Env = withEnv(os.Environ(), map[string]string{"CGO_ENABLED": "0"})
	if out, err := buildCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("go build failed: %w\n%s", err, string(out))
	}

	verOut, err := exec.Command(binPath, "--version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s --version failed: %w\n%s", binPath, err, string(verOut))
	}
	expectedVersion := strings.TrimSpace(string(verOut))

	var apiURL, tokenFile, agentLogPath string
	var agentPID int
	var agentCmd *exec.Cmd
	var agentDone chan error

	if *mode == "attached" {
		apiPort, err := freePort()
		if err != nil {
			return err
		}
		if apiPort == 8645 {
			apiPort, err = freePort()
			if err != nil {
				return err
			}
		}
		apiAddr := fmt.Sprintf("127.0.0.1:%d", apiPort)
		apiURL = "http://" + apiAddr
		tokenFile = filepath.Join(agentStateDir, "gui-token")
		agentLogPath = filepath.Join(runDir, "agent.log")
		agentLog, err := os.Create(agentLogPath)
		if err != nil {
			return err
		}
		agentCmd = exec.Command(binPath, "agent", "--demo", "--local", apiAddr, "--state", agentStateDir)
		agentCmd.Dir = runDir
		agentCmd.Stdout = agentLog
		agentCmd.Stderr = agentLog
		if err := agentCmd.Start(); err != nil {
			_ = agentLog.Close()
			return fmt.Errorf("start demo agent: %w", err)
		}
		_ = agentLog.Close()
		agentPID = agentCmd.Process.Pid
		startedPIDs = append(startedPIDs, agentPID)
		agentDone = make(chan error, 1)
		go func() { agentDone <- agentCmd.Wait() }()

		if err := waitAgentReady(apiURL, tokenFile, agentPID, *timeout); err != nil {
			return fmt.Errorf("agent not ready: %w\n%s", err, tailFile(agentLogPath, 30))
		}
	}

	autoPort, err := freePort()
	if err != nil {
		return err
	}
	autoAddr := fmt.Sprintf("127.0.0.1:%d", autoPort)
	guiLogPath := filepath.Join(runDir, "gui.log")
	guiLog, err := os.Create(guiLogPath)
	if err != nil {
		return err
	}

	var guiArgs []string
	if *mode == "attached" {
		guiArgs = []string{"--agent", apiURL, "--token-file", tokenFile, "--attach-only", "--state", guiStateDir}
	} else {
		guiArgs = []string{"--demo", "--state", guiStateDir}
	}

	guiCmd := exec.Command(binPath, guiArgs...)
	guiCmd.Dir = runDir
	guiCmd.Env = withEnv(os.Environ(), map[string]string{
		"MYGO_AUTOMATION": "tcp:" + autoAddr,
	})
	guiCmd.Stdout = guiLog
	guiCmd.Stderr = guiLog
	if err := guiCmd.Start(); err != nil {
		_ = guiLog.Close()
		return fmt.Errorf("start GUI: %w", err)
	}
	_ = guiLog.Close()
	guiPID := guiCmd.Process.Pid
	startedPIDs = append(startedPIDs, guiPID)
	guiDone := make(chan error, 1)
	go func() { guiDone <- guiCmd.Wait() }()

	if err := waitGUIReady(autoAddr, guiPID, *mode, *timeout); err != nil {
		return fmt.Errorf("GUI not ready: %w\n%s", err, tailFile(guiLogPath, 30))
	}

	sess := runSession{
		RunID:           *runID,
		Mode:            *mode,
		RepoRoot:        repoRoot,
		BinPath:         binPath,
		RunDir:          runDir,
		EvidenceDir:     evidenceDir,
		LauncherPID:     os.Getpid(),
		AgentPID:        agentPID,
		GUIPID:          guiPID,
		AutomationAddr:  autoAddr,
		APIURL:          apiURL,
		TokenFile:       tokenFile,
		AgentStateDir:   agentStateDir,
		GUIStateDir:     guiStateDir,
		ExpectedVersion: expectedVersion,
		StartedAt:       time.Now().Format(time.RFC3339),
	}
	raw, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return err
	}
	tmpSess := sessFile + ".tmp"
	if err := os.WriteFile(tmpSess, append(raw, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmpSess, sessFile); err != nil {
		_ = os.Remove(tmpSess)
		return err
	}

	rollback = false
	fmt.Println(string(raw))
	fmt.Println("READY")
	_ = os.Stdout.Sync()

	// Supervise children in foreground so Windows/POSIX process-group runners keep
	// the agent and GUI alive until `cleanup --run <id>` stops them or signal arrives.
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	select {
	case <-guiDone:
	case <-sigCh:
		_ = killPID(guiPID)
		select {
		case <-guiDone:
		case <-time.After(3 * time.Second):
		}
	}

	if agentPID > 0 {
		_ = killPID(agentPID)
		if agentDone != nil {
			select {
			case <-agentDone:
			case <-time.After(2 * time.Second):
			}
		}
	}
	return nil
}

func waitAgentReady(apiURL, tokenFile string, pid int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}
	for time.Now().Before(deadline) {
		if !pidAlive(pid) {
			return fmt.Errorf("agent process %d exited prematurely", pid)
		}
		tokBytes, err := os.ReadFile(tokenFile)
		token := strings.TrimSpace(string(tokBytes))
		if err == nil && token != "" {
			req, _ := http.NewRequest("GET", apiURL+"/v1/health", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			resp, err := client.Do(req)
			if err == nil {
				body, _ := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				if resp.StatusCode == http.StatusOK && strings.Contains(string(body), `"ok":true`) {
					infoReq, _ := http.NewRequest("GET", apiURL+"/v1/info", nil)
					infoReq.Header.Set("Authorization", "Bearer "+token)
					infoResp, err := client.Do(infoReq)
					if err == nil {
						infoBody, _ := io.ReadAll(infoResp.Body)
						_ = infoResp.Body.Close()
						var info struct {
							Mode string `json:"mode"`
						}
						if infoResp.StatusCode == http.StatusOK && json.Unmarshal(infoBody, &info) == nil && info.Mode == "demo" {
							return nil
						}
					}
				}
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	return fmt.Errorf("timed out after %s waiting for %s", timeout, apiURL)
}

func waitGUIReady(autoAddr string, pid int, mode string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := newAutoClient(autoAddr)
	expectedBadge := "演示模式"
	if mode == "attached" {
		expectedBadge = "已连接"
	}
	for time.Now().Before(deadline) {
		if !pidAlive(pid) {
			return fmt.Errorf("GUI process %d exited prematurely", pid)
		}
		winID, win, err := client.mainWindowID()
		if err == nil && win.Visible {
			nodes, err := client.tree(winID)
			if err == nil {
				hasAlpha := false
				hasHello := false
				hasBadge := false
				for _, n := range nodes {
					if n.Label == "alpha" || n.keyString() == "node-alpha" {
						hasAlpha = true
					}
					if n.Label == "hello.txt" {
						hasHello = true
					}
					if strings.Contains(n.Label, expectedBadge) {
						hasBadge = true
					}
				}
				if hasAlpha && hasHello && hasBadge {
					_ = client.waitIdle(winID, 1500)
					return nil
				}
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("timed out after %s waiting for Tailfile window & seeded demo files", timeout)
}

func cmdWaitReady(args []string) error {
	fs := flag.NewFlagSet("wait-ready", flag.ContinueOnError)
	runID := fs.String("run", "", "Verification run ID")
	timeout := fs.Duration("timeout", 60*time.Second, "Wait timeout for background launch")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *runID == "" {
		return errors.New("--run is required")
	}
	runDir, err := runDirFor(*runID)
	if err != nil {
		return err
	}
	sessFile := filepath.Join(runDir, "session.json")
	launchErrFile := filepath.Join(runDir, "launch.error")

	deadline := time.Now().Add(*timeout)
	for time.Now().Before(deadline) {
		if raw, err := os.ReadFile(launchErrFile); err == nil && len(bytes.TrimSpace(raw)) > 0 {
			return fmt.Errorf("background launch failed:\n%s", strings.TrimSpace(string(raw)))
		}
		if _, err := os.Stat(sessFile); err == nil {
			sess, err := loadSession(*runID)
			if err != nil {
				return err
			}
			checks, issues := checkSessionHealth(sess)
			if len(issues) > 0 {
				return fmt.Errorf("session %q became unhealthy: %s", *runID, strings.Join(issues, "; "))
			}
			raw, _ := json.MarshalIndent(sess, "", "  ")
			fmt.Println(string(raw))
			for _, c := range checks {
				fmt.Println("OK  ", c)
			}
			fmt.Println("READY")
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("timed out after %s waiting for %s", *timeout, sessFile)
}

func cmdDoctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	runID := fs.String("run", "", "Verification run ID")
	if err := fs.Parse(args); err != nil {
		return err
	}
	sess, err := loadSession(*runID)
	if err != nil {
		return err
	}
	checks, issues := checkSessionHealth(sess)
	for _, c := range checks {
		fmt.Println("OK  ", c)
	}
	if len(issues) > 0 {
		for _, is := range issues {
			fmt.Fprintln(os.Stderr, "FAIL", is)
		}
		return fmt.Errorf("doctor failed (%d issue(s))", len(issues))
	}
	fmt.Println("DOCTOR PASSED")
	return nil
}

func checkSessionHealth(sess runSession) (checks, issues []string) {

	// 1. Isolation check (never touch user live daemon/state)
	tmpClean := filepath.Clean(os.TempDir())
	runClean := filepath.Clean(sess.RunDir)
	if !strings.HasPrefix(strings.ToLower(runClean), strings.ToLower(tmpClean)) {
		issues = append(issues, fmt.Sprintf("runDir %s is not under temp dir %s", sess.RunDir, tmpClean))
	} else {
		checks = append(checks, "isolated runDir: "+sess.RunDir)
	}
	if strings.Contains(sess.APIURL, ":8645") {
		issues = append(issues, "apiURL uses default user daemon port 8645")
	}

	// 2. Binary & version check
	if _, err := os.Stat(sess.BinPath); err != nil {
		issues = append(issues, fmt.Sprintf("missing binary %s: %v", sess.BinPath, err))
	} else if out, err := exec.Command(sess.BinPath, "--version").CombinedOutput(); err != nil {
		issues = append(issues, fmt.Sprintf("version check failed: %v", err))
	} else {
		got := strings.TrimSpace(string(out))
		if got != sess.ExpectedVersion {
			issues = append(issues, fmt.Sprintf("version mismatch: got %q, want %q", got, sess.ExpectedVersion))
		} else {
			checks = append(checks, "binary version: "+got)
		}
	}

	// 3. Process liveness
	if sess.Mode == "attached" {
		if !pidAlive(sess.AgentPID) {
			issues = append(issues, fmt.Sprintf("agent PID %d is not running", sess.AgentPID))
		} else {
			checks = append(checks, fmt.Sprintf("agent PID %d alive", sess.AgentPID))
		}
	}
	if !pidAlive(sess.GUIPID) {
		issues = append(issues, fmt.Sprintf("GUI PID %d is not running", sess.GUIPID))
	} else {
		checks = append(checks, fmt.Sprintf("GUI PID %d alive", sess.GUIPID))
	}

	// 4. Agent HTTP API check (attached mode)
	if sess.Mode == "attached" {
		tokBytes, err := os.ReadFile(sess.TokenFile)
		if err != nil {
			issues = append(issues, fmt.Sprintf("read token file %s: %v", sess.TokenFile, err))
		} else {
			token := strings.TrimSpace(string(tokBytes))
			client := &http.Client{Timeout: 3 * time.Second}
			req, _ := http.NewRequest("GET", sess.APIURL+"/v1/info", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			resp, err := client.Do(req)
			if err != nil {
				issues = append(issues, fmt.Sprintf("GET /v1/info failed: %v", err))
			} else {
				body, _ := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				var info struct {
					Mode string `json:"mode"`
				}
				if resp.StatusCode != http.StatusOK {
					issues = append(issues, fmt.Sprintf("GET /v1/info HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body))))
				} else if json.Unmarshal(body, &info) != nil || info.Mode != "demo" {
					issues = append(issues, fmt.Sprintf("agent is NOT in demo isolation mode: %s", strings.TrimSpace(string(body))))
				} else {
					checks = append(checks, fmt.Sprintf("agent API %s healthy (mode=demo)", sess.APIURL))
				}
			}
		}
	}

	// 5. Automation window & tree check
	ac := newAutoClient(sess.AutomationAddr)
	winID, win, err := ac.mainWindowID()
	if err != nil {
		issues = append(issues, fmt.Sprintf("automation window check failed: %v", err))
	} else if !win.Visible {
		issues = append(issues, fmt.Sprintf("window %d (%q) is not visible", winID, win.Title))
	} else {
		nodes, err := ac.tree(winID)
		if err != nil {
			issues = append(issues, fmt.Sprintf("automation tree failed: %v", err))
		} else if len(nodes) == 0 {
			issues = append(issues, "automation tree returned 0 nodes")
		} else {
			checks = append(checks, fmt.Sprintf("window %q (%.0fx%.0f, %d ARIA nodes)", win.Title, win.Bounds.Width, win.Bounds.Height, len(nodes)))
		}
	}

	return checks, issues
}

func cmdSnapshot(args []string) error {
	fs := flag.NewFlagSet("snapshot", flag.ContinueOnError)
	runID := fs.String("run", "", "Verification run ID")
	outPath := fs.String("out", "", "Optional text file path for ARIA snapshot")
	jsonOut := fs.String("json-out", "", "Optional raw JSON file path for automation tree")
	if err := fs.Parse(args); err != nil {
		return err
	}
	sess, err := loadSession(*runID)
	if err != nil {
		return err
	}
	ac := newAutoClient(sess.AutomationAddr)
	winID, win, err := ac.mainWindowID()
	if err != nil {
		return err
	}
	nodes, err := ac.tree(winID)
	if err != nil {
		return err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# Window id=%d title=%q size=%.0fx%.0f nodes=%d\n", winID, win.Title, win.Bounds.Width, win.Bounds.Height, len(nodes))
	for _, n := range nodes {
		b.WriteString(n.summary())
		b.WriteByte('\n')
	}
	text := b.String()
	fmt.Print(text)

	if *outPath != "" {
		if err := ensureParentAndWrite(*outPath, []byte(text)); err != nil {
			return err
		}
	}
	if *jsonOut != "" {
		raw, err := json.MarshalIndent(map[string]any{"window": win, "nodes": nodes}, "", "  ")
		if err != nil {
			return err
		}
		if err := ensureParentAndWrite(*jsonOut, append(raw, '\n')); err != nil {
			return err
		}
	}
	return nil
}

type nodeSelector struct {
	id        string
	key       string
	keyPrefix string
	keySuffix string
	label     string
	role      string
	contains  string
	index     int
}

func findNode(nodes []treeNode, sel nodeSelector) (treeNode, error) {
	var matches []treeNode
	for _, n := range nodes {
		if n.Bounds.Width <= 0 || n.Bounds.Height <= 0 {
			continue
		}
		ks := n.keyString()
		if sel.id != "" && n.ID != sel.id {
			continue
		}
		if sel.key != "" && ks != sel.key {
			continue
		}
		if sel.keyPrefix != "" && !strings.HasPrefix(ks, sel.keyPrefix) {
			continue
		}
		if sel.keySuffix != "" && !strings.HasSuffix(ks, sel.keySuffix) {
			continue
		}
		if sel.label != "" && n.Label != sel.label {
			continue
		}
		if sel.role != "" && n.Role != sel.role {
			continue
		}
		if sel.contains != "" {
			hay := n.Label + "\n" + n.Value + "\n" + ks
			if !strings.Contains(hay, sel.contains) {
				continue
			}
		}
		matches = append(matches, n)
	}
	if len(matches) == 0 {
		return treeNode{}, fmt.Errorf("no visible node matched selector")
	}
	if sel.index >= 0 {
		if sel.index >= len(matches) {
			return treeNode{}, fmt.Errorf("matched %d nodes, index %d out of range", len(matches), sel.index)
		}
		return matches[sel.index], nil
	}
	if len(matches) > 1 {
		var lines []string
		for i, m := range matches {
			lines = append(lines, fmt.Sprintf("  [%d] %s", i, m.summary()))
		}
		return treeNode{}, fmt.Errorf("ambiguous selector matched %d nodes (narrow with --role/--key or pass --index):\n%s", len(matches), strings.Join(lines, "\n"))
	}
	return matches[0], nil
}

func cmdAct(args []string) error {
	fs := flag.NewFlagSet("act", flag.ContinueOnError)
	runID := fs.String("run", "", "Verification run ID")
	key := fs.String("key", "", "Exact element key")
	keySuffix := fs.String("key-suffix", "", "Element key suffix")
	label := fs.String("label", "", "Exact ARIA label")
	role := fs.String("role", "", "ARIA role")
	id := fs.String("id", "", "Exact node ID")
	action := fs.String("action", "press", "Action: press, focus, setValue, insertText, scrollIntoView, etc.")
	text := fs.String("text", "", "Text argument for setValue/insertText")
	index := fs.Int("index", -1, "0-based match index when multiple nodes match")
	if err := fs.Parse(args); err != nil {
		return err
	}
	sess, err := loadSession(*runID)
	if err != nil {
		return err
	}
	ac := newAutoClient(sess.AutomationAddr)
	winID, _, err := ac.mainWindowID()
	if err != nil {
		return err
	}
	nodes, err := ac.tree(winID)
	if err != nil {
		return err
	}
	target, err := findNode(nodes, nodeSelector{
		id:        *id,
		key:       *key,
		keySuffix: *keySuffix,
		label:     *label,
		role:      *role,
		index:     *index,
	})
	if err != nil {
		return err
	}
	params := map[string]any{
		"window": winID,
		"id":     target.ID,
		"action": *action,
	}
	if *action == "setValue" || *action == "insertText" || *text != "" {
		params["text"] = *text
	}
	if err := ac.call("act", params, nil); err != nil {
		return err
	}
	_ = ac.waitIdle(winID, 2500)
	fmt.Printf("act %s on %s\n", *action, target.summary())
	return nil
}

func cmdClick(args []string) error {
	fs := flag.NewFlagSet("click", flag.ContinueOnError)
	runID := fs.String("run", "", "Verification run ID")
	key := fs.String("key", "", "Exact element key")
	keyPrefix := fs.String("key-prefix", "", "Element key prefix")
	keySuffix := fs.String("key-suffix", "", "Element key suffix")
	label := fs.String("label", "", "Exact ARIA label")
	role := fs.String("role", "", "ARIA role (e.g. listItem, button)")
	contains := fs.String("contains", "", "Substring in label/value/key")
	index := fs.Int("index", -1, "0-based match index")
	clicks := fs.Int("clicks", 1, "Click count (1=single, 2=double)")
	button := fs.String("button", "left", "Mouse button: left, right, middle")
	modsRaw := fs.String("mods", "", "Comma-separated modifiers: shift,ctrl,alt,cmdOrCtrl")
	if err := fs.Parse(args); err != nil {
		return err
	}
	sess, err := loadSession(*runID)
	if err != nil {
		return err
	}
	ac := newAutoClient(sess.AutomationAddr)
	winID, _, err := ac.mainWindowID()
	if err != nil {
		return err
	}
	nodes, err := ac.tree(winID)
	if err != nil {
		return err
	}
	target, err := findNode(nodes, nodeSelector{
		key:       *key,
		keyPrefix: *keyPrefix,
		keySuffix: *keySuffix,
		label:     *label,
		role:      *role,
		contains:  *contains,
		index:     *index,
	})
	if err != nil {
		return err
	}
	cx, cy := target.center()
	ev := map[string]any{
		"type":   "click",
		"x":      cx,
		"y":      cy,
		"button": *button,
		"clicks": *clicks,
	}
	if strings.TrimSpace(*modsRaw) != "" {
		var mods []string
		for _, m := range strings.Split(*modsRaw, ",") {
			if m = strings.TrimSpace(m); m != "" {
				mods = append(mods, m)
			}
		}
		if len(mods) > 0 {
			ev["mods"] = mods
		}
	}
	if err := ac.call("input", map[string]any{
		"window": winID,
		"events": []any{ev},
	}, nil); err != nil {
		return err
	}
	_ = ac.waitIdle(winID, 2500)
	fmt.Printf("clicked (%dx %s) @(%.1f,%.1f) %s\n", *clicks, *button, cx, cy, target.summary())
	return nil
}

func cmdDrag(args []string) error {
	fs := flag.NewFlagSet("drag", flag.ContinueOnError)
	runID := fs.String("run", "", "Verification run ID")
	fromX := fs.Float64("from-x", 0, "Drag start X in window DIPs")
	fromY := fs.Float64("from-y", 0, "Drag start Y in window DIPs")
	toX := fs.Float64("to-x", 0, "Drag end X in window DIPs")
	toY := fs.Float64("to-y", 0, "Drag end Y in window DIPs")
	steps := fs.Int("steps", 6, "Intermediate pointerMove steps")
	modsRaw := fs.String("mods", "", "Comma-separated modifiers: shift,ctrl,alt,cmdOrCtrl")
	hold := fs.Bool("hold", false, "Leave primary mouse button held after moving (for mid-drag capture)")
	releaseOnly := fs.Bool("release-only", false, "Release primary mouse button at (--to-x, --to-y) after --hold")
	if err := fs.Parse(args); err != nil {
		return err
	}
	sess, err := loadSession(*runID)
	if err != nil {
		return err
	}
	ac := newAutoClient(sess.AutomationAddr)
	winID, _, err := ac.mainWindowID()
	if err != nil {
		return err
	}
	var mods []string
	if strings.TrimSpace(*modsRaw) != "" {
		for _, m := range strings.Split(*modsRaw, ",") {
			if m = strings.TrimSpace(m); m != "" {
				mods = append(mods, m)
			}
		}
	}
	ev := func(kind string, x, y float64) map[string]any {
		m := map[string]any{
			"type":   kind,
			"x":      x,
			"y":      y,
			"button": "left",
		}
		if kind == "pointerDown" || kind == "pointerUp" {
			m["clicks"] = 1
		}
		if len(mods) > 0 {
			m["mods"] = mods
		}
		return m
	}

	var events []any
	if *releaseOnly {
		events = append(events, ev("pointerMove", *toX, *toY), ev("pointerUp", *toX, *toY))
	} else {
		n := *steps
		if n < 1 {
			n = 1
		}
		events = append(events, ev("pointerMove", *fromX, *fromY), ev("pointerDown", *fromX, *fromY))
		for i := 1; i <= n; i++ {
			t := float64(i) / float64(n)
			events = append(events, ev("pointerMove", *fromX+(*toX-*fromX)*t, *fromY+(*toY-*fromY)*t))
		}
		if !*hold {
			events = append(events, ev("pointerUp", *toX, *toY))
		}
	}
	if err := ac.call("input", map[string]any{
		"window": winID,
		"events": events,
	}, nil); err != nil {
		return err
	}
	_ = ac.waitIdle(winID, 2500)
	switch {
	case *releaseOnly:
		fmt.Printf("released drag @(%.1f,%.1f)\n", *toX, *toY)
	case *hold:
		fmt.Printf("dragged (held) @(%.1f,%.1f)->@(%.1f,%.1f)\n", *fromX, *fromY, *toX, *toY)
	default:
		fmt.Printf("dragged @(%.1f,%.1f)->@(%.1f,%.1f)\n", *fromX, *fromY, *toX, *toY)
	}
	return nil
}

func cmdType(args []string) error {
	fs := flag.NewFlagSet("type", flag.ContinueOnError)
	runID := fs.String("run", "", "Verification run ID")
	text := fs.String("text", "", "Text to type into focused control")
	if err := fs.Parse(args); err != nil {
		return err
	}
	sess, err := loadSession(*runID)
	if err != nil {
		return err
	}
	ac := newAutoClient(sess.AutomationAddr)
	winID, _, err := ac.mainWindowID()
	if err != nil {
		return err
	}
	if err := ac.call("input", map[string]any{
		"window": winID,
		"events": []any{map[string]any{"type": "text", "text": *text}},
	}, nil); err != nil {
		return err
	}
	_ = ac.waitIdle(winID, 2500)
	fmt.Printf("typed %q\n", *text)
	return nil
}

func cmdKey(args []string) error {
	fs := flag.NewFlagSet("key", flag.ContinueOnError)
	runID := fs.String("run", "", "Verification run ID")
	accel := fs.String("key", "", "Key or accelerator (e.g. Enter, Escape, F2, Delete, CmdOrCtrl+T)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *accel == "" {
		return errors.New("--key is required")
	}
	sess, err := loadSession(*runID)
	if err != nil {
		return err
	}
	ac := newAutoClient(sess.AutomationAddr)
	winID, _, err := ac.mainWindowID()
	if err != nil {
		return err
	}
	if err := ac.call("input", map[string]any{
		"window": winID,
		"events": []any{map[string]any{"type": "key", "key": *accel}},
	}, nil); err != nil {
		return err
	}
	_ = ac.waitIdle(winID, 2500)
	fmt.Printf("pressed key %s\n", *accel)
	return nil
}

func cmdWait(args []string) error {
	fs := flag.NewFlagSet("wait", flag.ContinueOnError)
	runID := fs.String("run", "", "Verification run ID")
	contains := fs.String("contains", "", "Wait until tree contains substring in label/value/key")
	absent := fs.String("absent", "", "Wait until substring is absent from tree")
	key := fs.String("key", "", "Wait until element with exact key exists")
	absentKey := fs.String("absent-key", "", "Wait until element with exact key is absent")
	timeout := fs.Duration("timeout", 10*time.Second, "Wait timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *contains == "" && *absent == "" && *key == "" && *absentKey == "" {
		return errors.New("specify at least one condition (--contains, --absent, --key, --absent-key)")
	}
	sess, err := loadSession(*runID)
	if err != nil {
		return err
	}
	ac := newAutoClient(sess.AutomationAddr)
	deadline := time.Now().Add(*timeout)
	var lastTreeSummary string
	for {
		winID, _, err := ac.mainWindowID()
		if err == nil {
			nodes, err := ac.tree(winID)
			if err == nil {
				ok := true
				var allText strings.Builder
				hasKey := false
				hasAbsentKey := false
				for _, n := range nodes {
					ks := n.keyString()
					if *key != "" && ks == *key {
						hasKey = true
					}
					if *absentKey != "" && ks == *absentKey {
						hasAbsentKey = true
					}
					allText.WriteString(n.Label)
					allText.WriteByte('\n')
					allText.WriteString(n.Value)
					allText.WriteByte('\n')
					allText.WriteString(ks)
					allText.WriteByte('\n')
				}
				hay := allText.String()
				lastTreeSummary = hay
				if *contains != "" && !strings.Contains(hay, *contains) {
					ok = false
				}
				if *absent != "" && strings.Contains(hay, *absent) {
					ok = false
				}
				if *key != "" && !hasKey {
					ok = false
				}
				if *absentKey != "" && hasAbsentKey {
					ok = false
				}
				if ok {
					fmt.Println("wait condition satisfied")
					return nil
				}
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out after %s waiting for tree condition (last saw text snippet: %.400q)", *timeout, lastTreeSummary)
		}
		time.Sleep(150 * time.Millisecond)
	}
}

func cmdScreenshot(args []string) error {
	fs := flag.NewFlagSet("screenshot", flag.ContinueOnError)
	runID := fs.String("run", "", "Verification run ID")
	outPath := fs.String("out", "", "Output PNG path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *outPath == "" {
		return errors.New("--out is required")
	}
	sess, err := loadSession(*runID)
	if err != nil {
		return err
	}
	ac := newAutoClient(sess.AutomationAddr)
	winID, _, err := ac.mainWindowID()
	if err != nil {
		return err
	}
	var res struct {
		PNG    string `json:"png"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
	}
	if err := ac.call("capture", map[string]any{"window": winID}, &res); err != nil {
		return err
	}
	pngBytes, err := base64.StdEncoding.DecodeString(res.PNG)
	if err != nil {
		return fmt.Errorf("decode screenshot base64: %w", err)
	}
	w, h, err := checkPNGVariation(pngBytes)
	if err != nil {
		return err
	}
	if err := ensureParentAndWrite(*outPath, pngBytes); err != nil {
		return err
	}
	fmt.Printf("captured %s (%dx%d, %d bytes)\n", *outPath, w, h, len(pngBytes))
	return nil
}

func checkPNGVariation(pngBytes []byte) (int, int, error) {
	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		return 0, 0, fmt.Errorf("invalid captured PNG: %w", err)
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return w, h, fmt.Errorf("empty screenshot bounds %v", b)
	}
	first := img.At(b.Min.X, b.Min.Y)
	fr, fg, fb, fa := first.RGBA()
	stepX := max(1, w/16)
	stepY := max(1, h/16)
	for y := b.Min.Y; y < b.Max.Y; y += stepY {
		for x := b.Min.X; x < b.Max.X; x += stepX {
			r, g, bl, a := img.At(x, y).RGBA()
			if r != fr || g != fg || bl != fb || a != fa {
				return w, h, nil
			}
		}
	}
	return w, h, fmt.Errorf("captured PNG appears completely uniform/blank (%dx%d)", w, h)
}

func cmdAPI(args []string) error {
	fs := flag.NewFlagSet("api", flag.ContinueOnError)
	runID := fs.String("run", "", "Verification run ID")
	method := fs.String("method", "GET", "HTTP method")
	path := fs.String("path", "/v1/health", "API path and query string")
	body := fs.String("body", "", "Optional JSON request body")
	noAuth := fs.Bool("no-auth", false, "Omit Authorization Bearer header")
	allowErr := fs.Bool("allow-error", false, "Do not fail on HTTP >= 400")
	rawOut := fs.Bool("raw", false, "Print only response body")
	outPath := fs.String("out", "", "Optional output file for response transcript")
	if err := fs.Parse(args); err != nil {
		return err
	}
	sess, err := loadSession(*runID)
	if err != nil {
		return err
	}
	if sess.APIURL == "" {
		return fmt.Errorf("run %q was started in %s mode (no agent HTTP API); launch with --mode attached", sess.RunID, sess.Mode)
	}
	var reqBody io.Reader
	if *body != "" {
		reqBody = strings.NewReader(*body)
	}
	req, err := http.NewRequest(*method, strings.TrimRight(sess.APIURL, "/")+*path, reqBody)
	if err != nil {
		return err
	}
	if *body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if !*noAuth {
		tokBytes, err := os.ReadFile(sess.TokenFile)
		if err != nil {
			return fmt.Errorf("read token file %s: %w", sess.TokenFile, err)
		}
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(tokBytes)))
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	formatted := string(bytes.TrimSpace(respBytes))
	var pretty bytes.Buffer
	if json.Indent(&pretty, respBytes, "", "  ") == nil {
		formatted = pretty.String()
	}

	var output string
	if *rawOut {
		output = formatted
	} else {
		output = fmt.Sprintf("HTTP %d %s %s\n%s\n", resp.StatusCode, *method, *path, formatted)
	}
	fmt.Print(output)
	if *outPath != "" {
		if err := ensureParentAndWrite(*outPath, []byte(output)); err != nil {
			return err
		}
	}
	if !*allowErr && resp.StatusCode >= 400 {
		return fmt.Errorf("API returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func cmdCLI(args []string) (int, error) {
	fs := flag.NewFlagSet("cli", flag.ContinueOnError)
	runID := fs.String("run", "", "Verification run ID (uses built binary; if omitted builds nothing and requires --bin)")
	binFlag := fs.String("bin", "", "Override executable path")
	outPath := fs.String("out", "", "Optional transcript output path")
	timeout := fs.Duration("timeout", 20*time.Second, "CLI command timeout")
	allowNonZero := fs.Bool("allow-nonzero", false, "Return 0 even when child exits non-zero")
	if err := fs.Parse(args); err != nil {
		return 2, err
	}
	cmdArgs := fs.Args()
	binPath := *binFlag
	workDir := ""
	if binPath == "" {
		sess, err := loadSession(*runID)
		if err != nil {
			return 1, err
		}
		binPath = sess.BinPath
		workDir = sess.RunDir
	}

	cmd := exec.Command(binPath, cmdArgs...)
	if workDir != "" {
		cmd.Dir = workDir
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		return 1, err
	}
	go func() { done <- cmd.Wait() }()

	var runErr error
	select {
	case runErr = <-done:
	case <-time.After(*timeout):
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		<-done
		return 1, fmt.Errorf("CLI command timed out after %s", *timeout)
	}

	code := 0
	if runErr != nil {
		var ee *exec.ExitError
		if errors.As(runErr, &ee) {
			code = ee.ExitCode()
		} else {
			return 1, runErr
		}
	}

	var transcript strings.Builder
	fmt.Fprintf(&transcript, "$ %s %s\n", binPath, strings.Join(cmdArgs, " "))
	fmt.Fprintf(&transcript, "exit_code=%d\n", code)
	if stdout.Len() > 0 {
		transcript.WriteString("--- stdout ---\n")
		transcript.WriteString(stdout.String())
		if !strings.HasSuffix(stdout.String(), "\n") {
			transcript.WriteByte('\n')
		}
	}
	if stderr.Len() > 0 {
		transcript.WriteString("--- stderr ---\n")
		transcript.WriteString(stderr.String())
		if !strings.HasSuffix(stderr.String(), "\n") {
			transcript.WriteByte('\n')
		}
	}
	outText := transcript.String()
	fmt.Print(outText)
	if *outPath != "" {
		if err := ensureParentAndWrite(*outPath, []byte(outText)); err != nil {
			return 1, err
		}
	}
	if *allowNonZero {
		return 0, nil
	}
	return code, nil
}

func cmdCleanup(args []string) error {
	fs := flag.NewFlagSet("cleanup", flag.ContinueOnError)
	runID := fs.String("run", "", "Verification run ID")
	if err := fs.Parse(args); err != nil {
		return err
	}
	sess, err := loadSession(*runID)
	if err != nil {
		// If session.json is missing, still clean up empty/partial runDir if valid ID.
		dir, derr := runDirFor(*runID)
		if derr == nil {
			if _, serr := os.Stat(dir); serr == nil {
				_ = os.RemoveAll(dir)
				fmt.Printf("removed partial runDir %s (no session.json)\n", dir)
				return nil
			}
		}
		return err
	}

	var errs []string
	// Terminate ONLY exact PIDs owned by this session (never kill by process name).
	for _, entry := range []struct {
		name string
		pid  int
	}{
		{"GUI", sess.GUIPID},
		{"agent", sess.AgentPID},
	} {
		if entry.pid <= 0 {
			continue
		}
		if err := killPID(entry.pid); err != nil {
			errs = append(errs, fmt.Sprintf("stop %s pid %d: %v", entry.name, entry.pid, err))
		} else {
			fmt.Printf("stopped %s PID %d\n", entry.name, entry.pid)
		}
	}

	// Give the foreground launch supervisor a moment to exit naturally after GUI teardown;
	// force-kill only that exact LauncherPID if it stays alive.
	if sess.LauncherPID > 0 {
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) && pidAlive(sess.LauncherPID) {
			time.Sleep(100 * time.Millisecond)
		}
		if pidAlive(sess.LauncherPID) {
			if err := killPID(sess.LauncherPID); err != nil {
				errs = append(errs, fmt.Sprintf("stop launcher pid %d: %v", sess.LauncherPID, err))
			} else {
				fmt.Printf("stopped launcher PID %d\n", sess.LauncherPID)
			}
		} else {
			fmt.Printf("launcher PID %d exited\n", sess.LauncherPID)
		}
	}

	// Retry RemoveAll briefly on Windows while OS releases binary/log file locks.
	var rmErr error
	for i := 0; i < 15; i++ {
		rmErr = os.RemoveAll(sess.RunDir)
		if rmErr == nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if rmErr != nil {
		errs = append(errs, fmt.Sprintf("remove runDir %s: %v", sess.RunDir, rmErr))
	} else {
		fmt.Printf("removed runDir %s\n", sess.RunDir)
	}

	// Never delete evidenceDir; count surviving artifacts for verification.
	if sess.EvidenceDir != "" {
		count := 0
		_ = filepath.WalkDir(sess.EvidenceDir, func(_ string, d os.DirEntry, walkErr error) error {
			if walkErr == nil && d != nil && !d.IsDir() {
				count++
			}
			return nil
		})
		fmt.Printf("preserved evidenceDir %s (%d file(s))\n", sess.EvidenceDir, count)
	}

	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	if runtime.GOOS == "windows" {
		out, err := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid), "/FO", "CSV", "/NH").Output()
		if err != nil {
			return false
		}
		return strings.Contains(string(out), fmt.Sprintf(`"%d"`, pid))
	}
	return exec.Command("kill", "-0", strconv.Itoa(pid)).Run() == nil
}

func killPID(pid int) error {
	if pid <= 0 || !pidAlive(pid) {
		return nil
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	_ = p.Kill()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !pidAlive(pid) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("process %d still alive after kill", pid)
}

func ensureParentAndWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func tailFile(path string, maxLines int) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\r\n"), "\n")
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return "--- log tail (" + filepath.Base(path) + ") ---\n" + strings.Join(lines, "\n")
}
