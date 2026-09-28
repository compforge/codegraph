.PHONY: fmt fix lint test build test-corpus test-python-corpus setup-typescript-corpus test-typescript-corpus

PYTHON ?= python3
NODE ?= node

CORPUS ?= all
CORPUS_REPORT_DIR ?= $(CURDIR)/.corpus-results
CORPUS_REEVALUATE_DIR ?=
CORPUS_BASELINE_DIR ?=

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './.git/*')

fix: fmt
	ruff check --fix tests/corpus/python
	ruff format tests/corpus/python

lint:
	$(NODE) --check tests/corpus/typescript/oracle.mjs
	$(NODE) --check tests/corpus/typescript/oracle.test.mjs
	ruff check tests/corpus/python
	ruff format --check tests/corpus/python
	@test -z "$$(gofmt -l $$(find . -name '*.go' -not -path './.git/*'))"
	go vet ./...
	cd tests/corpus && go vet ./...

test:
	$(NODE) --test tests/corpus/typescript/oracle.test.mjs
	$(PYTHON) -m unittest discover -s tests/corpus/python -p 'test_*.py'
	go test -race -count=1 ./...
	cd tests/corpus && go test -race -count=1 ./...

build:
	go build ./...
	cd tests/corpus && go test -run '^$$' ./...

# Explicit network corpus; ordinary test runs only the evaluator's local contracts.
test-corpus:
	cd tests/corpus && go test -v -run '^TestRepositories$$' -count=1 -timeout=20m -python='$(PYTHON)' -node='$(NODE)' -corpus='$(CORPUS)' -report-dir='$(CORPUS_REPORT_DIR)' -baseline-dir='$(CORPUS_BASELINE_DIR)' -reevaluate-dir='$(CORPUS_REEVALUATE_DIR)'

# Pure source analysis: no package imports, application execution or services.
test-python-corpus:
	$(MAKE) test-corpus CORPUS=python-stdx,agentue

# Install only the pinned compiler oracle; never install target application code.
setup-typescript-corpus:
	cd tests/corpus/typescript && npm ci --ignore-scripts --no-audit --no-fund

test-typescript-corpus:
	$(MAKE) test-corpus CORPUS=doctor
