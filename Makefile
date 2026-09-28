.PHONY: fmt fix lint test build test-corpus test-python-corpus

PYTHON ?= python3

CORPUS ?= all
CORPUS_REPORT_DIR ?= $(CURDIR)/.corpus-results
CORPUS_BASELINE_DIR ?=

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './.git/*')

fix: fmt
	ruff check --fix tests/corpus/python
	ruff format tests/corpus/python

lint:
	ruff check tests/corpus/python
	ruff format --check tests/corpus/python
	@test -z "$$(gofmt -l $$(find . -name '*.go' -not -path './.git/*'))"
	go vet ./...
	cd tests/corpus && go vet ./...

test:
	$(PYTHON) -m unittest discover -s tests/corpus/python -p 'test_*.py'
	go test -race -count=1 ./...
	cd tests/corpus && go test -race -count=1 ./...

build:
	go build ./...
	cd tests/corpus && go test -run '^$$' ./...

# Explicit network corpus; ordinary test runs only the evaluator's local contracts.
test-corpus:
	cd tests/corpus && go test -v -run '^TestRepositories$$' -count=1 -timeout=20m -python='$(PYTHON)' -corpus='$(CORPUS)' -report-dir='$(CORPUS_REPORT_DIR)' -baseline-dir='$(CORPUS_BASELINE_DIR)'

# Pure source analysis: no package imports, application execution or services.
test-python-corpus:
	$(MAKE) test-corpus CORPUS=python-stdx,agentue
