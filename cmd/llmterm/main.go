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
	"strings"
	"syscall"

	"github.com/wei/llmterm/internal/backend/claude"
	"github.com/wei/llmterm/internal/render"
	"github.com/wei/llmterm/internal/session"
)

const (
	version = "0.1.0"
	// Read-only default tool set: safe to run without prompting.
	readOnlyTools = "Read Glob Grep WebFetch WebSearch"
	// Full set granted when the user opts in via `llm! ...` (Bash/Edit/Write enabled).
	writeTools = "Read Glob Grep WebFetch WebSearch Bash Edit Write"
)

func main() {
	if len(os.Args) < 2 {
		usage(os.Stderr)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "run":
		os.Exit(cmdRun(os.Args[2:]))
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
	fmt.Fprintf(w, `llmterm %s — terminal-native agent mode for your existing Claude Code

Commands:
  llmterm run [--unsafe] -- <prompt...>   Run one prompt against claude.
  llmterm doctor                          Check that claude is installed & authed.
  llmterm init zsh                        Print the zsh integration script.
  llmterm version                         Print version.

The zsh integration intercepts buffers starting with "llm " (read-only tools)
or "llm! " (also allows Bash/Edit/Write) and forwards the rest to llmterm run.
`, version)
}

func cmdRun(args []string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	unsafe := fs.Bool("unsafe", false, "allow Bash/Edit/Write tools (write mode)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	prompt := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if prompt == "" {
		fmt.Fprintln(os.Stderr, "llmterm run: empty prompt")
		return 2
	}

	cwd, _ := os.Getwd()
	store := session.Open(session.DefaultPath())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigC := make(chan os.Signal, 1)
	signal.Notify(sigC, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigC
		cancel()
	}()

	allowed := readOnlyTools
	if *unsafe {
		allowed = writeTools
	}

	opts := claude.Options{
		Prompt:       prompt,
		CWD:          cwd,
		AllowedTools: strings.Fields(allowed),
		ResumeID:     store.Resume(cwd),
	}

	events, errs, err := claude.Run(ctx, opts)
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
		store.Save(cwd, sid)
	}
	if !sawFinal {
		return 1
	}
	return 0
}

func cmdDoctor() int {
	fmt.Print("checking claude... ")
	out, err := exec.Command("claude", "--version").Output()
	if err != nil {
		fmt.Println("MISSING")
		fmt.Println("  install: https://docs.anthropic.com/claude/docs/claude-code")
		return 1
	}
	fmt.Print("ok (", strings.TrimSpace(string(out)), ")  ")

	fmt.Print("auth probe... ")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, "claude", "-p", "ok", "--output-format", "stream-json", "--include-partial-messages", "--verbose")
	stdout, _ := cmd.StdoutPipe()
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		fmt.Println("FAILED:", err)
		return 1
	}
	probeOK := false
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				if strings.Contains(string(buf[:n]), `"type":"result"`) {
					probeOK = true
					cancel()
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	_ = cmd.Wait()
	if !probeOK {
		fmt.Println("FAILED — try `claude /login`")
		return 1
	}
	fmt.Println("ok")
	fmt.Println("ready.")
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
  local prompt mode
  if [[ "$buf" == "llm! "* ]]; then
    prompt="${buf#llm! }"; mode="--unsafe"
  elif [[ "$buf" == "llm "* ]]; then
    prompt="${buf#llm }"; mode=""
  else
    zle accept-line
    return
  fi
  if [[ -z "$prompt" ]]; then
    zle accept-line
    return
  fi
  # Echo the original line so it appears in scrollback like a normal command.
  print -P "%B%F{cyan}❯%f%b $buf"
  BUFFER=""
  zle reset-prompt
  if [[ -n "$mode" ]]; then
    command llmterm run $mode -- "$prompt"
  else
    command llmterm run -- "$prompt"
  fi
  zle reset-prompt
}
zle -N __llmterm_dispatch
bindkey '^M' __llmterm_dispatch
`
