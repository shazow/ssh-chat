# ssh-chat

Custom SSH server written in Go. Instead of a shell, you get a chat prompt.


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


## Developing

If you're developing on this repo, there is a handy Makefile that should set
things up with `make run`.


## TODO:

* [x] Welcome message.
* [x] set term width properly
* [x] client map rather than list
* [x] backfill chat history
* [ ] tab completion
* [x] /ban
* [x] /help
* [x] /about
* [x] /list
* [x] /nick
* [x] pubkey fingerprint
* [x] truncate usernames
* [ ] rename collision bug
* [x] Some tests.
* [ ] More tests.
* [ ] Even more tests.
* [ ] Lots of refactoring
  * [ ] Pull out the chat-related stuff into isolation from the ssh serving
    stuff


## License

MIT
