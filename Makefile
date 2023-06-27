VERSION := $(shell git describe --tags --always --dirty="-dev")

all: clean build

v:
	@echo "Version: ${VERSION}"

clean:
	rm -rf build/

build:
	go build -trimpath -ldflags "-X main.version=${VERSION}" -v -o build/node cmd/node/main.go

test:
	go test ./...

test-race:
	go test -race ./...

lint:
	gofmt -d -s .
	gofumpt -d -extra .
	go vet ./...
	staticcheck ./...
	golangci-lint run

fmt:
	gofumpt -l -w -extra .

cover:
	go test -coverprofile=/tmp/go-sim-lb.cover.tmp ./...
	go tool cover -func /tmp/go-sim-lb.cover.tmp
	unlink /tmp/go-sim-lb.cover.tmp

cover-html:
	go test -coverprofile=/tmp/go-sim-lb.cover.tmp ./...
	go tool cover -html=/tmp/go-sim-lb.cover.tmp
	unlink /tmp/go-sim-lb.cover.tmp

docker-image:
	DOCKER_BUILDKIT=1 docker build --platform linux/amd64 --build-arg VERSION=${VERSION} . -t mev-share-node:${VERSION}
