BINARY = ssh-chat
KEY = host_key
PORT = 2022

SRCS = %.go
VERSION := $(shell git describe --tags --dirty --always 2> /dev/null || echo "dev")
LDFLAGS = -X main.Version=$(VERSION) -extldflags "-static"

all: $(BINARY)

$(BINARY): **/**/*.go **/*.go *.go
	go build $(BUILDFLAGS) ./cmd/ssh-chat

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

release:
	# We use static linking for release build. LDFLAGS via
	# https://github.com/golang/go/issues/26492
	# Can replace LDFLAGS with -static once the issue has been resolved.
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 LDFLAGS='$(LDFLAGS)' ./build_release "github.com/shazow/ssh-chat/cmd/ssh-chat" README.md LICENSE
	CGO_ENABLED=0 GOOS=linux GOARCH=386 LDFLAGS='$(LDFLAGS)' ./build_release "github.com/shazow/ssh-chat/cmd/ssh-chat" README.md LICENSE
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=6 LDFLAGS='$(LDFLAGS)' ./build_release "github.com/shazow/ssh-chat/cmd/ssh-chat" README.md LICENSE
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 LDFLAGS='$(LDFLAGS)' ./build_release "github.com/shazow/ssh-chat/cmd/ssh-chat" README.md LICENSE
	CGO_ENABLED=0 GOOS=freebsd GOARCH=amd64 LDFLAGS='$(LDFLAGS)' ./build_release "github.com/shazow/ssh-chat/cmd/ssh-chat" README.md LICENSE
	CGO_ENABLED=0 GOOS=windows GOARCH=386 LDFLAGS='$(LDFLAGS)' ./build_release "github.com/shazow/ssh-chat/cmd/ssh-chat" README.md LICENSE
