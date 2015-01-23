BINARY = ssh-chat
KEY = host_key
PORT = 2022

SRCS = %.go

all: $(BINARY)

$(BINARY): **/**/*.go **/*.go *.go
	go build -ldflags "-X main.buildCommit `git describe --long --tags --dirty --always`" ./cmd/ssh-chat

deps:
	go get .

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
