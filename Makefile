.PHONY: fmt lint test build

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './.git/*')

lint:
	@test -z "$$(gofmt -l $$(find . -name '*.go' -not -path './.git/*'))"
	go vet ./...

test:
	go test -race -count=1 ./...

build:
	go build ./...
