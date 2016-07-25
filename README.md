[![Build Status](https://travis-ci.org/shazow/ssh-chat.svg?branch=master)](https://travis-ci.org/shazow/ssh-chat)
[![Bountysource](https://www.bountysource.com/badge/team?team_id=52292&style=bounties_received)](https://www.bountysource.com/teams/ssh-chat/issues?utm_source=ssh-chat&utm_medium=shield&utm_campaign=bounties_received)
[![GoDoc](https://godoc.org/github.com/shazow/ssh-chat?status.svg)](https://godoc.org/github.com/shazow/ssh-chat)

# ssh-chat

Custom SSH server written in Go. Instead of a shell, you get a chat prompt.

## Demo

Join the party:

```
$ ssh chat.shazow.net
```

The server's RSA key fingerprint is `e5:d5:d1:75:90:38:42:f6:c7:03:d7:d0:56:7d:6a:db`. If you see something different, you might be [MITM](https://en.wikipedia.org/wiki/Man-in-the-middle_attack)'d.

(Apologies if the server is down, try again shortly.)


## Downloading a release

Recent releases include builds for MacOS (darwin/amd64) and Linux (386,
amd64, and ARM6 for your RaspberryPi).

**[Grab the latest release here](https://github.com/shazow/ssh-chat/releases/)**.


## Compiling / Developing

You can compile ssh-chat by using `make build`. The resulting binary is portable and
can be run on any system with a similar OS and CPU arch. Go 1.3 or higher is required to compile.

If you're developing on this repo, there is a handy Makefile that should set
things up with `make run`.

Additionally, `make debug` runs the server with an http `pprof` server. This allows you to open
[http://localhost:6060/debug/pprof/]() and view profiling data. See
[net/http/pprof](http://golang.org/pkg/net/http/pprof/) for more information about `pprof`.


## Quick Start

```
Usage:
  ssh-chat [OPTIONS]

Application Options:
  -v, --verbose    Show verbose logging.
  -i, --identity=  Private key to identify server with. (~/.ssh/id_rsa)
      --bind=      Host and port to listen on. (0.0.0.0:2022)
      --admin=     Fingerprint of pubkey to mark as admin.
      --whitelist= Optional file of pubkey fingerprints that are allowed to connect
      --motd=      Message of the Day file (optional)
      --pprof=     enable http server for pprof

Help Options:
  -h, --help       Show this help message
```

After doing `go get github.com/shazow/ssh-chat/...` on this repo, you should be able
to run a command like:

```
$ ssh-chat --verbose --bind ":22" --identity ~/.ssh/id_dsa
```

To bind on port 22, you'll need to make sure it's free (move any other ssh
daemons to another port) and run ssh-chat as root (or with sudo).


## License

This project is licensed under the MIT open source license.
