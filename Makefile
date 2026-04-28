GO ?= go
GOLANGCI_LINT ?= golangci-lint

.PHONY: lint test testacc gofmt-check

lint: gofmt-check
	$(GOLANGCI_LINT) run ./...

gofmt-check:
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "gofmt required:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

test:
	$(GO) test ./...

testacc:
	TF_ACC=1 $(GO) test -tags=acc ./... -count=1
