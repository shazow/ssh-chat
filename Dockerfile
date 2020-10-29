FROM golang:alpine AS builder

WORKDIR /usr/src/app

COPY . .
RUN apk add make openssh
RUN make build


FROM alpine

RUN apk add openssh
RUN mkdir /root/.ssh
WORKDIR /root/.ssh
RUN ssh-keygen -t rsa -C "chatkey" -f id_rsa

WORKDIR /usr/local/bin

COPY --from=builder /usr/src/app/ssh-chat .
RUN chmod +x ssh-chat
CMD ["/usr/local/bin/ssh-chat"]
