#!/bin/bash -ex
CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -ldflags "-X main.buildCommit `git describe --long --tags --dirty --always`" ./cmd/ssh-chat
docker login -e="$DOCKER_EMAIL" -u="$DOCKER_USER" -p="$DOCKER_PASS"
docker push $DOCKER_USER/ssh-chat:latest
