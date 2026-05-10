PREFIX ?= $(HOME)/.local
BIN    := $(PREFIX)/bin/llmterm

.PHONY: build install test clean uninstall

build:
	go build -o bin/llmterm ./cmd/llmterm

test:
	go test ./...

install: build
	@mkdir -p $(PREFIX)/bin
	install -m 0755 bin/llmterm $(BIN)
	@echo
	@echo "installed: $(BIN)"
	@echo
	@echo "add this to ~/.zshrc (idempotent):"
	@echo
	@echo "    eval \"\$$($(BIN) init zsh)\""
	@echo
	@echo "then open a new shell and run: llmterm doctor"

uninstall:
	rm -f $(BIN)

clean:
	rm -rf bin
