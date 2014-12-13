FROM golang:1.3.3
MAINTAINER Alvin Lai <al@alvinlai.com>

RUN apt-get update
RUN apt-get install -y openssh-client

RUN go get github.com/shazow/ssh-chat
RUN ssh-keygen -f ~/.ssh/id_rsa -t rsa -N ''

EXPOSE 2022

CMD ["-i", "/root/.ssh/id_rsa", "-vv", "--bind", "\":2022\""]
ENTRYPOINT ["ssh-chat"]
