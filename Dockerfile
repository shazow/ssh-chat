#
# Usage example:
# $ docker build -t ssh-chat .
# $ docker run -d -p 0.0.0.0:(your host machine port):2022 --name ssh-chat ssh-chat
#
FROM golang:1.4
MAINTAINER Alvin Lai <al@alvinlai.com>

RUN apt-get update
RUN apt-get install -y openssh-client

RUN go get github.com/shazow/ssh-chat
RUN ssh-keygen -f ~/.ssh/id_rsa -t rsa -N ''

EXPOSE 2022

CMD ["-i", "/root/.ssh/id_rsa", "-vv", "--bind", "\":2022\""]
ENTRYPOINT ["ssh-chat"]
