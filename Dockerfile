FROM golang:1.7-alpine

RUN apk add --no-cache openssh git

RUN adduser -S ssh-chat

WORKDIR /home/ssh-chat
ENV HOME /home/ssh-chat

RUN go get -v github.com/shazow/ssh-chat
RUN go build github.com/shazow/ssh-chat/cmd/ssh-chat

RUN mv ./ssh-chat /usr/bin/ssh-chat && \
    echo -e '#!/bin/sh\nif [[ ! -e ~/.ssh/id_rsa ]]; then ssh-keygen -t rsa -N "" -f ~/.ssh/id_rsa; fi; /usr/bin/ssh-chat $@' > /usr/bin/ssh-chat-run && \
    chmod +x /usr/bin/ssh-chat-run

USER ssh-chat

ENTRYPOINT ["/usr/bin/ssh-chat-run"]
CMD ["--verbose --bind :5000 --identity ~/.ssh/id_rsa"]
