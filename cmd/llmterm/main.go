package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"sort"
	"strings"
	"syscall"

	"github.com/wei/llmterm/internal/backend"
	_ "github.com/wei/llmterm/internal/backend/claude"
	_ "github.com/wei/llmterm/internal/backend/codex"
	_ "github.com/wei/llmterm/internal/backend/gemini"
	"github.com/wei/llmterm/internal/config"
	"github.com/wei/llmterm/internal/render"
	"github.com/wei/llmterm/internal/session"
)

const version = "0.2.0"

func main() {
	if len(os.Args) < 2 {
		usage(os.Stderr)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "run":
		os.Exit(cmdRun(os.Args[2:]))
	case "use":
		os.Exit(cmdUse(os.Args[2:]))
	case "onboard":
		os.Exit(cmdOnboard(os.Args[2:]))
	case "doctor":
		os.Exit(cmdDoctor())
	case "init":
		os.Exit(cmdInit(os.Args[2:]))
	case "version", "-v", "--version":
		fmt.Println("llmterm", version)
	case "help", "-h", "--help":
		usage(os.Stdout)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		usage(os.Stderr)
		os.Exit(2)
	}
}

func usage(w io.Writer) {
	fmt.Fprintf(w, `llmterm %s — terminal-native agent mode for your existing Claude / Codex / Gemini CLI

Commands:
  llmterm onboard [--yes] [--default <backend>]
                                          First-use wizard: probe backends,
                                          pick a default, set up ~/.zshrc.
  llmterm run [--unsafe] -- <prompt...>   Run one prompt against the selected backend.
  llmterm use [claude|codex|gemini]       Switch backend (no arg = claude).
  llmterm doctor                          Check the active backend is installed & authed.
  llmterm init zsh                        Print the zsh integration script.
  llmterm version                         Print version.

The zsh integration intercepts buffers starting with "llm " (read-only tools)
or "llm! " (also allows Bash/Edit/Write) and forwards the rest to llmterm run.
`, version)
}

func cmdRun(args []string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	unsafe := fs.Bool("unsafe", false, "allow Bash/Edit/Write tools")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	prompt := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if prompt == "" {
		fmt.Fprintln(os.Stderr, "llmterm run: empty prompt")
		return 2
	}

	cwd, _ := os.Getwd()
	cfg := config.Load()
	be, err := backend.Get(cfg.Backend)
	if err != nil {
		fmt.Fprintln(os.Stderr, "llmterm:", err)
		return 1
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigC := make(chan os.Signal, 1)
	signal.Notify(sigC, os.Interrupt, syscall.SIGTERM)
	go func() { <-sigC; cancel() }()

	store := session.Open(session.DefaultPath())
	resumeKey := cfg.Backend + ":" + cwd

	events, errs, err := be.Run(ctx, backend.Options{
		Prompt:   prompt,
		CWD:      cwd,
		Unsafe:   *unsafe,
		ResumeID: store.Resume(resumeKey),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "llmterm:", err)
		return 1
	}

	r := render.New(os.Stdout, isTTY(os.Stdout))
	var sid string
	var sawFinal bool
	for events != nil || errs != nil {
		select {
		case ev, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			s, done := r.Handle(ev)
			if s != "" {
				sid = s
			}
			if done {
				sawFinal = true
			}
		case e, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			if e != nil && !errors.Is(e, context.Canceled) {
				r.Errorf("%v", e)
			}
		}
	}
	if sid != "" {
		store.Save(resumeKey, sid)
	}
	if !sawFinal {
		return 1
	}
	return 0
}

func cmdUse(args []string) int {
	name := "claude"
	if len(args) > 0 && args[0] != "" {
		name = args[0]
	}
	be, err := backend.Get(name)
	if err != nil {
		fmt.Fprintln(os.Stderr, "llmterm use:", err)
		return 2
	}

	availErr := be.Available(context.Background())
	cfg := config.Load()
	cfg.Backend = name
	if err := config.Save(cfg); err != nil {
		fmt.Fprintln(os.Stderr, "llmterm use: save failed:", err)
		return 1
	}

	printSwitchBanner(name, availErr)
	return 0
}

// printSwitchBanner draws a small confirmation card identifying the backend
// the user just switched to, plus the detected CLI version and a "third-party
// wrapper" footer for compliance. We DO NOT reproduce the upstream CLI's own
// welcome screen — only refer to it nominatively.
func printSwitchBanner(name string, availErr error) {
	color := backendColor(name)
	display, vendor := backendDisplay(name)
	bin := backendBin(name)
	version := detectVersion(bin)

	if !isTTY(os.Stdout) {
		// Plain output in non-interactive contexts (CI, pipes).
		fmt.Printf("backend: %s (%s) via %s %s\n", name, vendor, bin, version)
		if availErr != nil {
			fmt.Fprintln(os.Stderr, "warn:", availErr)
		}
		return
	}

	const w = 56
	top := "╭" + strings.Repeat("─", w-2) + "╮"
	bot := "╰" + strings.Repeat("─", w-2) + "╯"
	mid := func(left, right string) string {
		pad := w - 4 - len(stripANSI(left)) - len(right)
		if pad < 1 {
			pad = 1
		}
		return "│ " + left + strings.Repeat(" ", pad) + right + " │"
	}

	bold := "\x1b[1m"
	dim := "\x1b[2m"
	reset := "\x1b[0m"

	// Compose all lines into one buffer and write atomically. When invoked
	// from a zle widget, splitting this across multiple Println calls races
	// with `zle reset-prompt`, occasionally clipping the bottom border.
	var b strings.Builder
	line := func(s string) { b.WriteString(color); b.WriteString(s); b.WriteString(reset); b.WriteByte('\n') }
	line(top)
	line(mid(bold+"llmterm"+reset+color+" → "+bold+display+reset+color, vendor))
	if version != "" {
		line(mid(dim+"via "+bin+" "+version+reset+color, ""))
	} else if availErr != nil {
		line(mid(dim+"CLI not installed"+reset+color, ""))
	}
	line(mid(dim+"third-party wrapper · not affiliated"+reset+color, ""))
	line(bot)
	// Trailing blank line: when this is invoked from a zle widget, the
	// `zle reset-prompt` that fires on widget exit redraws the prompt on
	// whatever row the cursor is on. Without this extra newline, the redraw
	// can land on top of the bottom border and clip it. The blank row gives
	// reset-prompt its own line to draw on.
	b.WriteByte('\n')
	_, _ = os.Stdout.WriteString(b.String())
	_ = os.Stdout.Sync()

	if availErr != nil {
		fmt.Fprintln(os.Stderr, "warn:", availErr)
		fmt.Fprintln(os.Stderr, "(saved; install the CLI then run `llmterm doctor`)")
	}
}

func backendColor(name string) string {
	switch name {
	case "claude":
		return "\x1b[38;5;208m" // Anthropic-ish orange
	case "codex":
		return "\x1b[38;5;48m" // OpenAI-ish teal/green
	case "gemini":
		return "\x1b[38;5;75m" // Google-ish blue
	}
	return "\x1b[37m"
}

func backendDisplay(name string) (display, vendor string) {
	switch name {
	case "claude":
		return "claude", "Anthropic"
	case "codex":
		return "codex", "OpenAI"
	case "gemini":
		return "gemini", "Google"
	}
	return name, ""
}

func backendBin(name string) string {
	// Currently 1:1 between backend name and CLI binary name.
	return name
}

func detectVersion(bin string) string {
	if _, err := exec.LookPath(bin); err != nil {
		return ""
	}
	out, err := exec.Command(bin, "--version").CombinedOutput()
	if err != nil {
		return ""
	}
	v := strings.TrimSpace(string(out))
	// Take the first line; some CLIs print extra info.
	if i := strings.IndexByte(v, '\n'); i >= 0 {
		v = v[:i]
	}
	if len(v) > 32 {
		v = v[:32]
	}
	return v
}

func stripANSI(s string) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		if r == '\x1b' {
			in = true
			continue
		}
		if in {
			if r == 'm' {
				in = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func cmdDoctor() int {
	cfg := config.Load()
	fmt.Printf("active backend: %s\n", cfg.Backend)

	names := backend.Names()
	sort.Strings(names)
	for _, n := range names {
		b, _ := backend.Get(n)
		marker := "  "
		if n == cfg.Backend {
			marker = "* "
		}
		fmt.Printf("%s%s: ", marker, n)
		if err := b.Available(context.Background()); err != nil {
			fmt.Println("MISSING —", err)
			continue
		}
		fmt.Println("ok")
	}
	return 0
}

func cmdInit(args []string) int {
	if len(args) == 0 || args[0] != "zsh" {
		fmt.Fprintln(os.Stderr, "usage: llmterm init zsh")
		return 2
	}
	_, _ = os.Stdout.WriteString(zshPlugin)
	return 0
}

func isTTY(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

const zshPlugin = `# llmterm zsh integration — source via: eval "$(llmterm init zsh)"
__llmterm_dispatch() {
  emulate -L zsh
  local buf="$BUFFER"
  if [[ -z "$buf" ]]; then
    zle accept-line
    return
  fi
  local args mode is_use
  if [[ "$buf" == "llm! "* ]]; then
    args="${buf#llm! }"; mode="--unsafe"
  elif [[ "$buf" == "llm "* ]]; then
    args="${buf#llm }"; mode=""
  elif [[ "$buf" == "llm" ]]; then
    args=""; mode=""
  else
    zle accept-line
    return
  fi
  # Bare "llm" with nothing after it falls through to claude usage hint.
  BUFFER=""
  zle -I
  print -rP -- "%B%F{cyan}❯%f%b $buf"
  # First token decides whether this is an llmterm subcommand or a prompt.
  local first="${args%% *}" rest=""
  if [[ "$args" == *" "* ]]; then rest="${args#* }"; fi
  case "$first" in
    "")
      command llmterm help
      ;;
    use|doctor|version|help|init|onboard)
      # Forward as llmterm subcommand. ${=rest} re-splits remaining args.
      command llmterm "$first" ${=rest}
      ;;
    *)
      if [[ -n "$mode" ]]; then
        command llmterm run $mode -- "$args"
      else
        command llmterm run -- "$args"
      fi
      ;;
  esac
  zle reset-prompt
}
zle -N __llmterm_dispatch
bindkey '^M' __llmterm_dispatch
`
