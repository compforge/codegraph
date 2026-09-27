.PHONY: fmt lint test build test-corpus

CORPUS ?= all
CORPUS_REPORT_DIR ?= $(CURDIR)/.corpus-results
CORPUS_BASELINE_DIR ?=

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './.git/*')

lint:
	@test -z "$$(gofmt -l $$(find . -name '*.go' -not -path './.git/*'))"
	go vet ./...
	cd tests/corpus && go vet ./...

test:
	go test -race -count=1 ./...
	cd tests/corpus && go test -race -count=1 ./...

build:
	go build ./...
	cd tests/corpus && go test -run '^$$' ./...

# Explicit network corpus; ordinary test runs only the evaluator's local contracts.
test-corpus:
	cd tests/corpus && go test -v -run '^TestRepositories$$' -count=1 -timeout=20m -corpus='$(CORPUS)' -report-dir='$(CORPUS_REPORT_DIR)' -baseline-dir='$(CORPUS_BASELINE_DIR)'
