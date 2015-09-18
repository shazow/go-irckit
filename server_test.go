package irckit

import (
	"testing"

	"github.com/sorcix/irc"
)

func TestServer(t *testing.T) {
	send, receive := make(chan *irc.Message, 10), make(chan *irc.Message, 10)
	u := NewUserMock(send, receive)

	srv := NewServer("irc.testserver.local")
	go srv.Connect(u)
	defer srv.Close()

	receive <- irc.ParseMessage("NICK foo")
	receive <- irc.ParseMessage("USER root 0 * :Foo Bar")
	if msg := <-send; msg.Command != irc.RPL_WELCOME {
		t.Errorf("got %v; want %v", msg, irc.RPL_WELCOME)
	}
}
