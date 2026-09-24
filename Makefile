.PHONY: build test vet fmt clean install web

BIN := bin/nomad
PKG := ./...
WEB_DIST := internal/webview/dist/index.html

# The embedded single-page app is a gitignored build artifact. Build
# it on demand (fresh clone, after frontend edits); a committed
# placeholder keeps plain `go build`/`go test` working without node.
$(WEB_DIST):
	cd internal/webview/frontend && (npm ci --no-audit --no-fund || npm install --no-audit --no-fund) && npm run build

web:
	cd internal/webview/frontend && (npm ci --no-audit --no-fund || npm install --no-audit --no-fund) && npm run build

build: $(WEB_DIST)
	go build -o $(BIN) ./cmd/nomad

install: $(WEB_DIST)
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
