package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
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
	ctx, cancel := context.WithTimeout(context.Background(), 0)
	cancel()
	_ = ctx
	if availErr := be.Available(context.Background()); availErr != nil {
		fmt.Fprintln(os.Stderr, "warn:", availErr)
		fmt.Fprintln(os.Stderr, "(saved anyway; install the CLI then run `llmterm doctor`)")
	}
	cfg := config.Load()
	cfg.Backend = name
	if err := config.Save(cfg); err != nil {
		fmt.Fprintln(os.Stderr, "llmterm use: save failed:", err)
		return 1
	}
	fmt.Printf("backend: %s\n", name)
	return 0
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
  if [[ "$args" == "use"* ]]; then
    # Subcommand: switch backend. Arg shape: "use" or "use <name>".
    local sub="${args#use}"; sub="${sub# }"
    command llmterm use $sub
  elif [[ -z "$args" ]]; then
    command llmterm help
  else
    if [[ -n "$mode" ]]; then
      command llmterm run $mode -- "$args"
    else
      command llmterm run -- "$args"
    fi
  fi
  zle reset-prompt
}
zle -N __llmterm_dispatch
bindkey '^M' __llmterm_dispatch
`
