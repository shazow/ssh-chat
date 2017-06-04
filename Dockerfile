FROM golang:1.8

WORKDIR /go/src/github.com/shazow/ssh-chat

COPY . .

RUN make

CMD ["make", "run"]
