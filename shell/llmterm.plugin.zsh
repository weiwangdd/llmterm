# llmterm zsh integration
# Prefer sourcing the live output of `llmterm init zsh` so this stays in sync
# with the binary. Drop this file if you want a static copy instead.
if command -v llmterm >/dev/null 2>&1; then
  eval "$(llmterm init zsh)"
fi
