/bin/bash -ex
test $TRAVIS_PULL_REQUEST == "false" && test $TRAVIS_BRANCH == "master"
docker login -e="$DOCKER_EMAIL" -u="$DOCKER_USER" -p="$DOCKER_PASS"
docker push $DOCKER_USER/ssh-chat:latest
