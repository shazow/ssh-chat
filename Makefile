BINARY = ssh-chat
KEY = host_key
PORT = 2022

SRCS = %.go
VERSION := $(shell git describe --tags --dirty --always)
LDFLAGS = LDFLAGS="-X main.Version=$(VERSION)"

all: $(BINARY)

$(BINARY): deps **/**/*.go **/*.go *.go
	go build $(BUILDFLAGS) ./cmd/ssh-chat

deps:
	go get ./...

build: $(BINARY)

clean:
	rm $(BINARY)

$(KEY):
	ssh-keygen -f $(KEY) -P ''

run: $(BINARY) $(KEY)
	./$(BINARY) -i $(KEY) --bind ":$(PORT)" -vv

debug: $(BINARY) $(KEY)
	./$(BINARY) --pprof 6060 -i $(KEY) --bind ":$(PORT)" -vv

test:
	go test ./...
	golint ./...

release:
	GOOS=linux GOARCH=amd64 $(LDFLAGS) ./build_release "github.com/shazow/ssh-chat/cmd/ssh-chat" README.md LICENSE
	GOOS=linux GOARCH=386 $(LDFLAGS) ./build_release "github.com/shazow/ssh-chat/cmd/ssh-chat" README.md LICENSE
	GOOS=darwin GOARCH=amd64 $(LDFLAGS) ./build_release "github.com/shazow/ssh-chat/cmd/ssh-chat" README.md LICENSE
