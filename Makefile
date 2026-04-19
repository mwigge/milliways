VERSION := $(shell git describe --tags --always --dirty 2>/dev/null)
LDFLAGS := -X main.version=$(VERSION)

.PHONY: smoke install

smoke:
	go build -ldflags "$(LDFLAGS)" -o $(TMPDIR)/milliways ./cmd/milliways
	VERSION=$(VERSION) scripts/smoke.sh

install:
	go build -ldflags "$(LDFLAGS)" -o ~/.local/bin/milliways ./cmd/milliways