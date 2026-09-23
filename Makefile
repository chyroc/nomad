.PHONY: build test vet fmt clean install

BIN := bin/nomad
PKG := ./...

build:
	go build -o $(BIN) ./cmd/nomad

install:
	go install ./cmd/nomad

test:
	go test $(PKG)

test-race:
	go test -race $(PKG)

vet:
	go vet $(PKG)

fmt:
	gofmt -w .

clean:
	rm -rf bin
