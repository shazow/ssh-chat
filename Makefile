BINARY = ssh-chat
KEY = host_key
PORT = 2022

all: $(BINARY)

**/*.go:
	go build ./...

$(BINARY): **/*.go *.go
	go build .

build: $(BINARY)

clean:
	rm $(BINARY)

$(KEY):
	ssh-keygen -f $(KEY) -P ''

run: $(BINARY) $(KEY)
	./$(BINARY) -i $(KEY) -b ":$(PORT)" -vv
