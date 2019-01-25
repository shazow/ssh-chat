/*
Package sshchat is an implementation of an ssh server which serves a chat room
instead of a shell.

sshd subdirectory contains the ssh-related pieces which know nothing about chat.

chat subdirectory contains the chat-related pieces which know nothing about ssh.

The Host type is the glue between the sshd and chat pieces.
*/
package sshchat
