VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-s -w -X github.com/cloud-exit/exitbox/cmd.Version=$(VERSION)"
BINARY := exitbox

.PHONY: build test vet lint clean install cross-compile

build:
	go build $(LDFLAGS) -o $(BINARY) .

test:
	go test ./...

vet:
	go vet ./...

lint:
	go vet ./...
	golint ./...

clean:
	rm -f $(BINARY)
	rm -f $(BINARY)-*

install: build
	install -d $(HOME)/.local/bin
	install -m 755 $(BINARY) $(HOME)/.local/bin/$(BINARY)

# Cross-compile for all release targets
cross-compile:
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BINARY)-linux-amd64 .
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o $(BINARY)-linux-arm64 .
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o $(BINARY)-darwin-amd64 .
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o $(BINARY)-darwin-arm64 .
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(BINARY)-windows-amd64.exe .
	GOOS=windows GOARCH=arm64 go build $(LDFLAGS) -o $(BINARY)-windows-arm64.exe .

checksums:
	sha256sum $(BINARY)-* > checksums.txt
