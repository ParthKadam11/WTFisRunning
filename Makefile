.PHONY: build test vet fmt run

build:
	go build -o wtfisrunning ./cmd/wtfisrunning

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

run:
	go run ./cmd/wtfisrunning --once
