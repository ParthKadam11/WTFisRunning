.PHONY: build test vet fmt run

build:
	go build -o wtf ./cmd/wtf

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

run:
	go run ./cmd/wtf --once
