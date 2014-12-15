[![Build Status](https://travis-ci.org/shazow/ssh-chat.svg?branch=master)](https://travis-ci.org/shazow/ssh-chat)

# ssh-chat

Custom SSH server written in Go. Instead of a shell, you get a chat prompt.

## Demo

Join the party:

```
$ ssh chat.shazow.net
```

The server's RSA key fingerprint is `e5:d5:d1:75:90:38:42:f6:c7:03:d7:d0:56:7d:6a:db`. If you see something different, you might be [MITM](https://en.wikipedia.org/wiki/Man-in-the-middle_attack)'d.

(Apologies if the server is down, try again shortly.)


## Quick Start

```
Usage:
  ssh-chat [OPTIONS]

Application Options:
  -v, --verbose   Show verbose logging.
  -b, --bind=     Host and port to listen on. (0.0.0.0:22)
  -i, --identity= Private key to identify server with. (~/.ssh/id_rsa)

Help Options:
  -h, --help      Show this help message
```

After doing `go get github.com/shazow/ssh-chat` on this repo, you should be able
to run a command like:

```
$ ssh-chat --verbose --bind ":2022" --identity ~/.ssh/id_dsa
```

To bind on port 22, you'll need to make sure it's free (move any other ssh
daemons to another port) and run ssh-chat as root (or with sudo).

## Deploying with Docker

You can run ssh-chat using a Docker image without manually installing go-lang:

```
$ docker pull alvin/ssh-chat
$ docker run -d -p 0.0.0.0:(your host machine port):2022 --name ssh-chat alvin/ssh-chat
```

See notes in the header of our Dockerfile for details on building your own image.


## Developing

If you're developing on this repo, there is a handy Makefile that should set
things up with `make run`.


## License

MIT
