.PHONY: build install

BIN := docker-pull-shim
INSTALL_DIR := $(HOME)/.local/bin

build:
	go build -o $(BIN) ./cmd/docker-pull-shim

install: build
	install -Dm755 $(BIN) $(INSTALL_DIR)/$(BIN)
