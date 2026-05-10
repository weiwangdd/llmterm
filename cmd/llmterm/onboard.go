package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/weiwangdd/llmterm/internal/backend"
	"github.com/weiwangdd/llmterm/internal/config"
)

// cmdOnboard runs the first-use wizard:
//   1. probe each backend (install + auth)
//   2. let the user pick a default backend
//   3. ensure the zsh integration line is in ~/.zshrc
//   4. print a 30-second sample
func cmdOnboard(args []string) int {
	defaultPick := ""
	yes := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--default":
			if i+1 < len(args) {
				defaultPick = args[i+1]
				i++
			}
		case "--yes", "-y":
			yes = true
		}
	}

	fmt.Println()
	fmt.Println("\x1b[1mllmterm onboarding\x1b[0m — let's get you set up.")
	fmt.Println()

	probes := []probeResult{}
	for _, name := range []string{"claude", "codex", "gemini"} {
		probes = append(probes, probeBackend(name))
	}
	printProbeTable(probes)

	usable := []string{}
	for _, p := range probes {
		if p.installed && p.authOK {
			usable = append(usable, p.name)
		}
	}

	if len(usable) == 0 {
		fmt.Println()
		fmt.Println("No usable backend yet. Install at least one of:")
		fmt.Println("  claude   https://docs.anthropic.com/claude/docs/claude-code")
		fmt.Println("  codex    https://github.com/openai/codex")
		fmt.Println("  gemini   https://github.com/google-gemini/gemini-cli")
		fmt.Println("Then run `llmterm onboard` again.")
		return 1
	}

	pick := defaultPick
	if pick == "" {
		if len(usable) == 1 {
			pick = usable[0]
		} else if yes {
			pick = usable[0]
		} else {
			pick = askChoice("Pick default backend", usable, usable[0])
		}
	}
	if !contains(usable, pick) {
		fmt.Fprintf(os.Stderr, "selected backend %q is not usable\n", pick)
		return 2
	}
	cfg := config.Load()
	cfg.Backend = pick
	if err := config.Save(cfg); err != nil {
		fmt.Fprintln(os.Stderr, "save config:", err)
		return 1
	}
	fmt.Printf("\n✓ default backend: \x1b[1m%s\x1b[0m  (saved to %s)\n", pick, config.Path())

	zshrc := zshrcPath()
	added, err := ensureZshrcLine(zshrc, yes)
	switch {
	case err != nil:
		fmt.Fprintln(os.Stderr, "zshrc:", err)
	case added:
		fmt.Printf("✓ added eval line to %s (backup: %s.bak)\n", zshrc, zshrc)
	default:
		fmt.Printf("✓ %s already sources llmterm\n", zshrc)
	}

	fmt.Println()
	fmt.Println("\x1b[1mTry it\x1b[0m — open a new shell (`exec zsh`) and:")
	fmt.Println("  llm   what files are in this directory")
	fmt.Println("  llm!  show current system memory usage")
	if len(usable) > 1 {
		fmt.Printf("  llm use %s\n", other(usable, pick))
	}
	fmt.Println()
	return 0
}

type probeResult struct {
	name      string
	installed bool
	version   string
	authOK    bool
	authErr   string
}

func probeBackend(name string) probeResult {
	r := probeResult{name: name}
	be, err := backend.Get(name)
	if err != nil {
		return r
	}
	if err := be.Available(context.Background()); err != nil {
		return r
	}
	r.installed = true
	r.version = detectVersion(name)

	// Auth probe: a tiny prompt with a 25s timeout. Backend-specific so we
	// can recognise success without depending on the renderer.
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	switch name {
	case "claude":
		out, err := exec.CommandContext(ctx, "claude", "-p", "ok",
			"--output-format", "stream-json", "--include-partial-messages", "--verbose").Output()
		if err == nil && strings.Contains(string(out), `"type":"result"`) {
			r.authOK = true
		} else if err != nil {
			r.authErr = "run `claude /login`"
		}
	case "codex":
		out, err := exec.CommandContext(ctx, "codex", "exec", "--json",
			"--skip-git-repo-check", "-s", "read-only", "ok").Output()
		if err == nil && strings.Contains(string(out), `"turn.completed"`) {
			r.authOK = true
		} else if err != nil {
			r.authErr = "run `codex login`"
		}
	case "gemini":
		err := exec.CommandContext(ctx, "gemini", "--prompt", "ok").Run()
		if err == nil {
			r.authOK = true
		} else {
			r.authErr = "run `gemini auth login`"
		}
	}
	return r
}

func printProbeTable(rs []probeResult) {
	fmt.Printf("  %-8s %-9s %-32s %s\n", "BACKEND", "INSTALLED", "VERSION", "AUTH")
	for _, r := range rs {
		inst := "✗"
		if r.installed {
			inst = "✓"
		}
		auth := "—"
		if r.installed {
			if r.authOK {
				auth = "✓"
			} else if r.authErr != "" {
				auth = "✗ " + r.authErr
			} else {
				auth = "✗"
			}
		}
		ver := r.version
		if ver == "" {
			ver = "-"
		}
		fmt.Printf("  %-8s %-9s %-32s %s\n", r.name, inst, ver, auth)
	}
}

func askChoice(prompt string, choices []string, def string) string {
	r := bufio.NewReader(os.Stdin)
	for {
		fmt.Printf("\n%s [%s]: ", prompt, strings.Join(choices, "/"))
		line, _ := r.ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" {
			return def
		}
		if contains(choices, line) {
			return line
		}
	}
}

func askYesNo(prompt string, def bool) bool {
	r := bufio.NewReader(os.Stdin)
	hint := "y/N"
	if def {
		hint = "Y/n"
	}
	fmt.Printf("%s [%s]: ", prompt, hint)
	line, _ := r.ReadString('\n')
	line = strings.ToLower(strings.TrimSpace(line))
	if line == "" {
		return def
	}
	return line == "y" || line == "yes"
}

func zshrcPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".zshrc")
}

const zshrcMarker = `eval "$(`
const zshrcLineSuffix = `llmterm init zsh)"`

// ensureZshrcLine appends the llmterm eval line to ~/.zshrc if it's not
// already present. Returns (added, err). Asks before modifying unless
// `force` is true. Always writes a .bak before changing.
func ensureZshrcLine(path string, force bool) (bool, error) {
	body, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if strings.Contains(string(body), zshrcLineSuffix) {
		return false, nil
	}
	if !force {
		fmt.Println()
		if !askYesNo(fmt.Sprintf("Append `eval \"$(llmterm init zsh)\"` to %s?", path), true) {
			fmt.Printf("(skipped — add it manually later: echo 'eval \"$(llmterm init zsh)\"' >> %s)\n", path)
			return false, nil
		}
	}
	if len(body) > 0 {
		bak := path + ".bak"
		if werr := os.WriteFile(bak, body, 0o644); werr != nil {
			return false, fmt.Errorf("backup: %w", werr)
		}
	}
	bin, _ := exec.LookPath("llmterm")
	if bin == "" {
		bin = "llmterm"
	}
	line := fmt.Sprintf("\n# added by llmterm onboard\neval \"$(%s init zsh)\"\n", bin)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return false, err
	}
	defer f.Close()
	if _, err := f.WriteString(line); err != nil {
		return false, err
	}
	return true, nil
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if x == y {
			return true
		}
	}
	return false
}

func other(xs []string, except string) string {
	for _, y := range xs {
		if y != except {
			return y
		}
	}
	return except
}
