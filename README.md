# llmterm

> [中文](README.zh.md) / English

> **Disclaimer.** llmterm is an unofficial, third-party tool. It invokes the
> Claude Code / Codex / Gemini CLI that **you** install and authenticate on
> your own machine, using **your** subscription. It is not affiliated with,
> endorsed by, or sponsored by Anthropic, OpenAI, or Google. llmterm never
> reads, stores, transmits, or proxies your credentials — it only spawns the
> upstream CLI as a subprocess. Account sharing, credential redistribution,
> and reselling access through llmterm are violations of the upstream
> providers' Terms of Service and are not supported.

Terminal-native agent mode for your existing Claude Code subscription.

Type `llm <natural language>` (or `llm! ...` if the task may write/run commands)
at your zsh prompt — llmterm forwards the request to the `claude` CLI you're
already logged in to, streams the agent loop into your terminal, and drops you
back at the prompt when it's done. Works in iTerm2, Ghostty, Terminal.app, and
any other emulator, because llmterm is just an ordinary command.

## Install (macOS, zsh)

Prereqs: Go ≥ 1.22 and at least one upstream agent CLI installed +
authenticated:
- [Claude Code](https://docs.anthropic.com/claude/docs/claude-code) (default backend)
- [Codex CLI](https://github.com/openai/codex) (optional)
- [Gemini CLI](https://github.com/google-gemini/gemini-cli) (optional)

**One-liner**:

```sh
curl -fsSL https://raw.githubusercontent.com/weiwangdd/llmterm/main/install.sh | bash
```

This `go install`s the binary and runs `llmterm onboard` so you can pick a
backend and (optionally) wire up `~/.zshrc`. Then open a new shell.

**From source**:

```sh
git clone https://github.com/weiwangdd/llmterm
cd llmterm && make install
echo 'eval "$($HOME/.local/bin/llmterm init zsh)"' >> ~/.zshrc
exec zsh
llmterm doctor
```

## Usage

```sh
llm  what's in this directory
llm  summarize the last 5 commits
llm! rename all .jpeg files here to .jpg
llm! create a python venv and install ruff
```

- `llm ` — read-only tools allowed (Read, Glob, Grep, WebFetch, WebSearch).
- `llm!` — also allows Bash, Edit, Write. Use only when you want the agent to
  change files or run commands.

`Ctrl-C` mid-stream cancels cleanly.

Per-directory session continuity is automatic: a follow-up `llm ...` in the
same directory within 2 hours resumes the previous conversation. State lives
at `~/.local/state/llmterm/sessions.json`.

## How it works

`llmterm run -- "<prompt>"` execs `claude -p "<prompt>" --output-format
stream-json --include-partial-messages --add-dir $(pwd) --allowedTools "..."`
and renders the streamed events (assistant text, tool calls, tool results,
final summary) to your TTY. No PTY interception, no API key, no extra billing.

## Scope (MVP)

Supported: macOS, zsh, Claude Code backend. bash/fish, Codex/Gemini backends,
and Linux/Windows are planned for v2.

## License

MIT.
