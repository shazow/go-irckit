package irckit

import (
	"testing"
	"time"

	"github.com/sorcix/irc"
)

const testServerName = "testserver"

var expectTimeout = time.Second * 1

func expectReply(t *testing.T, conn *mockConn, expect string) {
	select {
	case msg := <-conn.send:
		if msg.String() != expect {
			t.Errorf("got %v; want %v", msg, expect)
		}
	case <-time.After(expectTimeout):
		t.Fatalf("timed out waiting for %v", expect)
	}
}

func TestServerWelcome(t *testing.T) {
	srv := NewServer(testServerName)
	defer srv.Close()

	send, receive := make(chan *irc.Message, 10), make(chan *irc.Message, 10)
	u := NewUserMock(send, receive)
	go srv.Connect(u)

	receive <- irc.ParseMessage("NICK foo")
	receive <- irc.ParseMessage("USER root 0 * :Foo Bar")
	if msg := <-send; msg.Command != irc.RPL_WELCOME {
		t.Errorf("got %v; want %v", msg, irc.RPL_WELCOME)
	}
}

func TestServerMultiUser(t *testing.T) {
	srv := NewServer(testServerName)
	defer srv.Close()

	u1 := NewConnMock("client1", 10)
	u2 := NewConnMock("client2", 10)

	go srv.Connect(NewUser(u1))
	go srv.Connect(NewUser(u2))

	u1.receive <- irc.ParseMessage("NICK foo")
	u1.receive <- irc.ParseMessage("USER root 0 * :Foo Bar")
	u2.receive <- irc.ParseMessage("NICK baz")
	u2.receive <- irc.ParseMessage("USER root 0 * :Baz Quux")

	expectReply(t, u1, ":testserver 001 foo :Welcome!")
	expectReply(t, u2, ":testserver 001 baz :Welcome!")

	u1.receive <- irc.ParseMessage("JOIN #chat")
	expectReply(t, u1, ":foo!root@client1 JOIN #chat")
	expectReply(t, u1, ":testserver 353 foo = #chat :foo")
	expectReply(t, u1, ":testserver 366 foo :End of /NAMES list.")
	expectReply(t, u1, ":testserver 331 #chat :No topic is set")

	u2.receive <- irc.ParseMessage("JOIN #chat")
	expectReply(t, u2, ":baz!root@client2 JOIN #chat")
	expectReply(t, u2, ":testserver 353 baz = #chat :baz foo")
	expectReply(t, u2, ":testserver 366 baz :End of /NAMES list.")
	expectReply(t, u2, ":testserver 331 #chat :No topic is set")

	expectReply(t, u1, ":baz!root@client2 JOIN #chat")

	u1.receive <- irc.ParseMessage("NICK foo_")
	expectReply(t, u1, ":foo!root@client1 NICK foo_")
	// FIXME:
	//expectReply(t, u2, ":foo!root@client1 NICK foo_")
}
