# Bauen des itop-agent und des Collectors.
#
# Go muss nicht lokal installiert sein: alle Ziele laufen in einem Container.
# Wer Go lokal hat, setzt GO=go und spart den Container-Aufruf.

VERSION ?= 0.4.0
GOIMAGE ?= golang:1.23-alpine
LDFLAGS  = -s -w -X main.Version=$(VERSION)

# In den Container gehoben, damit Modul- und Build-Cache Laeufe ueberdauern.
GO ?= docker run --rm \
        -v $(CURDIR):/src -w /src \
        -v itop-agent-gocache:/root/.cache/go-build \
        -v itop-agent-gomod:/go/pkg/mod \
        -e CGO_ENABLED=0 $(GOIMAGE) go

.PHONY: all
all: build-linux build-windows build-collector

.PHONY: build-linux
build-linux:
	$(GO) build -trimpath -ldflags="$(LDFLAGS)" -o dist/itop-agent ./cmd/agent

.PHONY: build-windows
build-windows:
	docker run --rm -v $(CURDIR):/src -w /src \
	  -v itop-agent-gocache:/root/.cache/go-build -v itop-agent-gomod:/go/pkg/mod \
	  -e CGO_ENABLED=0 -e GOOS=windows -e GOARCH=amd64 $(GOIMAGE) \
	  go build -trimpath -ldflags="$(LDFLAGS)" -o dist/itop-agent.exe ./cmd/agent

.PHONY: build-collector
build-collector:
	$(GO) build -trimpath -ldflags="$(LDFLAGS)" -o dist/itop-collector ./cmd/collector

.PHONY: image
image:
	docker build -t itop-collector:$(VERSION) -f deploy/collector/Dockerfile .

# Prueft BEIDE Plattformen. Ein Fehler in collect_windows.go faellt beim reinen
# Linux-Build nicht auf - die Datei wird dort gar nicht uebersetzt.
.PHONY: vet
vet:
	$(GO) vet ./...
	docker run --rm -v $(CURDIR):/src -w /src \
	  -v itop-agent-gocache:/root/.cache/go-build -v itop-agent-gomod:/go/pkg/mod \
	  -e GOOS=windows -e GOARCH=amd64 $(GOIMAGE) go vet ./...

.PHONY: test
test:
	$(GO) test ./...

.PHONY: fmt
fmt:
	$(GO) fmt ./...

.PHONY: check
check: fmt vet test

# .deb/.rpm bauen. Braucht nfpm; laeuft ebenfalls im Container.
.PHONY: packages
packages: build-linux
	docker run --rm -v $(CURDIR):/src -w /src -e VERSION=$(VERSION) \
	  goreleaser/nfpm package -f deploy/linux/nfpm.yaml -p deb -t dist/
	docker run --rm -v $(CURDIR):/src -w /src -e VERSION=$(VERSION) \
	  goreleaser/nfpm package -f deploy/linux/nfpm.yaml -p rpm -t dist/

.PHONY: clean
clean:
	rm -rf dist/
