.PHONY: smoke

smoke:
	@go build -o $(TMPDIR)milliways ./cmd/milliways
	@scripts/smoke.sh
