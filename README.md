[![Build Status](https://travis-ci.org/shazow/ssh-chat.svg?branch=master)](https://travis-ci.org/shazow/ssh-chat)
[![GoDoc](https://godoc.org/github.com/shazow/ssh-chat?status.svg)](https://godoc.org/github.com/shazow/ssh-chat)
[![Downloads](https://img.shields.io/github/downloads/shazow/ssh-chat/total.svg?color=orange)](https://github.com/shazow/ssh-chat/releases)
[![Bountysource](https://www.bountysource.com/badge/team?team_id=52292&style=bounties_received)](https://www.bountysource.com/teams/ssh-chat/issues?utm_source=ssh-chat&utm_medium=shield&utm_campaign=bounties_received)


# ssh-chat

Custom SSH server written in Go. Instead of a shell, you get a chat prompt.

## Demo

Join the party:

```
$ ssh ssh.chat
```

The server's RSA key fingerprint is `MD5:e5:d5:d1:75:90:38:42:f6:c7:03:d7:d0:56:7d:6a:db` or `SHA256:HQDLlZsXL3t0lV5CHM0OXeZ5O6PcfHuzkS8cRbbTLBI`. If you see something different, you might be [MITM](https://en.wikipedia.org/wiki/Man-in-the-middle_attack)'d.

(Apologies if the server is down, try again shortly.)


## Downloading a release

Recent releases include builds for MacOS (darwin/amd64) and Linux (386,
amd64, and ARM6 for your RaspberryPi).

**[Grab the latest binary release here](https://github.com/shazow/ssh-chat/releases/)**.

Play around with it. Additional [deploy examples are here](https://github.com/shazow/ssh-chat/wiki/Deployment).


## Compiling / Developing

Most people just want the [latest binary release](https://github.com/shazow/ssh-chat/releases/). If you're sure you want to compile it from source, read on:

You can compile ssh-chat by using `make build`. The resulting binary is portable and
can be run on any system with a similar OS and CPU arch. Go 1.8 or higher is required to compile.

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
      --version    Print version and exit.
  -i, --identity=  Private key to identify server with. (default: ~/.ssh/id_rsa)
      --bind=      Host and port to listen on. (default: 0.0.0.0:2022)
      --admin=     File of public keys who are admins.
      --whitelist= Optional file of public keys who are allowed to connect.
      --motd=      Optional Message of the Day file.
      --log=       Write chat log to this file.
      --pprof=     Enable pprof http server for profiling.

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

## Frequently Asked Questions

The FAQs can be found on the project's [Wiki page](https://github.com/shazow/ssh-chat/wiki/FAQ).
Feel free to submit more questions to be answered and added to the page.

## License

This project is licensed under the MIT open source license.
