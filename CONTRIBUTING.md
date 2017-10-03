# How to Contribute

This is a brief guide on how you can contribute to `ssh-chat`

## Getting Started

Contributions come in the form of bug reports, feature requests, documentation and wiki edits, and pull requests. If you have an issue with a certain feature or encountered a bug, you will refer to the Issues section.

### Submitting an Issue

`ssh-chat` has a lot of issues, and we try to help every one of them as best we can. The best way to submit an issue is simple: check if it already exists using the search bar. If you encounter a bug or want a certain feature, make sure no one else has submitted it before so we can avoid duplicate issues.

When submitting a bug report, make sure you submit very specific details surrounding the bug:

* What did you do to create the bug?
* Was there any error code given or exceptions thrown?
* What operating system are you and which version of OpenSSH are you using?
* If you built from source, what version of Golang did you use to build `ssh-chat`?

These details should help us to come to a solution.

For feature requests, use the search bar to look up if a feature you want has already been requested. If there was an issue already create, you can vote on it using the "thumbs up" emoji.

### Submitting Code

Submitting code is another way to contribute. The best way to start contributing code would be to look at all the open Issues and see if you can find an interesting bug to tackle. Or if there's a feature you want to implement, check if an Issue was opened for it, or even submit the feature request yourself to open up a discussion.

When submitting code, you should, in your commit message, refer to which issue you are working on. That way when the issue is resolved, or if future bugs are introduced because of it, we can refer to the pull request made and try to fix any bugs.

Once submitted, the code must meet the following conditions in order to be accepted:
* Code must be formatted using `gofmt`
* Code must pass code review
* Code must pass the Travis CI testing stage

If the code meets these conditions, then it will be merged into the `master` branch.


### Discussion Channels

Development discussion of `ssh-chat` can be found on Shazow's public `ssh-chat` server. Connect using any `ssh` client with the following:

```bash
$ ssh username@chat.shazow.net
```
