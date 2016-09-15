BINARY = ssh-chat
KEY = host_key
PORT = 2022

SRCS = %.go
VERSION := $(shell git describe --tags --dirty --always 2> /dev/null || echo "dev")
LDFLAGS = LDFLAGS="-X main.Version=$(VERSION)"

SUBPACKAGES := $(shell go list ./... | grep -v /vendor/)

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
	go test -v $(SUBPACKAGES)

release:
	GOOS=linux GOARCH=arm GOARM=6 $(LDFLAGS) ./build_release "github.com/shazow/ssh-chat/cmd/ssh-chat" README.md LICENSE
	GOOS=linux GOARCH=amd64 $(LDFLAGS) ./build_release "github.com/shazow/ssh-chat/cmd/ssh-chat" README.md LICENSE
	GOOS=linux GOARCH=386 $(LDFLAGS) ./build_release "github.com/shazow/ssh-chat/cmd/ssh-chat" README.md LICENSE
	GOOS=darwin GOARCH=amd64 $(LDFLAGS) ./build_release "github.com/shazow/ssh-chat/cmd/ssh-chat" README.md LICENSE
	GOOS=freebsd GOARCH=amd64 $(LDFLAGS) ./build_release "github.com/shazow/ssh-chat/cmd/ssh-chat" README.md LICENSE
	GOOS=windows GOARCH=386 $(LDFLAGS) ./build_release "github.com/shazow/ssh-chat/cmd/ssh-chat" README.md LICENSE
