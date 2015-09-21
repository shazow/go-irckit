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
			t.Errorf("\ngot\t\t%q\nwant\t%q", msg, expect)
		}
	case <-time.After(expectTimeout):
		t.Fatalf("timed out waiting for %q", expect)
	}
}

func expectEvent(t *testing.T, events <-chan Event, expect EventKind) {
	select {
	case evt := <-events:
		if evt.Kind() != expect {
			t.Errorf("got %q; expected %s", evt, expect)
		}
	case <-time.After(expectTimeout):
		t.Fatalf("timed out waiting for %q", expect)
	}
}

func TestServerWelcome(t *testing.T) {
	events := make(chan Event, 10)
	srv := NewServer(testServerName)
	srv.Subscribe(events)
	defer srv.Close()

	send, receive := make(chan *irc.Message, 10), make(chan *irc.Message, 10)
	u := NewUserMock(send, receive)
	go srv.Connect(u)
	receive <- irc.ParseMessage("NICK foo")
	receive <- irc.ParseMessage("USER root 0 * :Foo Bar")
	if msg := <-send; msg.Command != irc.RPL_WELCOME {
		t.Errorf("got %v; want %v", msg, irc.RPL_WELCOME)
	}
	expectEvent(t, events, ConnectEvent)
}

func TestServerMultiUser(t *testing.T) {
	events := make(chan Event, 10)
	srv := NewServer(testServerName)
	srv.Subscribe(events)
	defer srv.Close()

	c1 := NewConnMock("client1", 10)
	c2 := NewConnMock("client2", 10)

	go srv.Connect(NewUser(c1))
	go srv.Connect(NewUser(c2))

	c1.receive <- irc.ParseMessage("NICK foo")
	c1.receive <- irc.ParseMessage("USER root 0 * :Foo Bar")
	expectEvent(t, events, ConnectEvent)
	c2.receive <- irc.ParseMessage("NICK baz")
	c2.receive <- irc.ParseMessage("USER root 0 * :Baz Quux")
	expectEvent(t, events, ConnectEvent)

	expectReply(t, c1, ":testserver 001 foo :Welcome!")
	expectReply(t, c2, ":testserver 001 baz :Welcome!")

	c1.receive <- irc.ParseMessage("JOIN #chat")
	expectReply(t, c1, ":foo!root@client1 JOIN #chat")
	expectReply(t, c1, ":testserver 353 foo = #chat :foo")
	expectReply(t, c1, ":testserver 366 foo :End of /NAMES list.")
	expectReply(t, c1, ":testserver 331 #chat :No topic is set")
	expectEvent(t, events, JoinEvent)

	c2.receive <- irc.ParseMessage("JOIN #chat")
	expectReply(t, c2, ":baz!root@client2 JOIN #chat")
	expectReply(t, c2, ":testserver 353 baz = #chat :baz foo")
	expectReply(t, c2, ":testserver 366 baz :End of /NAMES list.")
	expectReply(t, c2, ":testserver 331 #chat :No topic is set")
	expectEvent(t, events, JoinEvent)

	// c1 notification of c2
	expectReply(t, c1, ":baz!root@client2 JOIN #chat")

	u1, ok := srv.HasUser("foo")
	if !ok {
		t.Fatal("server did not recognize user with nick: foo")
	}
	if len(u1.Channels()) != 1 {
		t.Errorf("expected 1 channel for foo; got: %v", u1.Channels())
	}
	channel := srv.Channel("#chat")
	if channel.Len() != 2 {
		t.Errorf("expected #chat to be len 2; got: %v", channel.Users())
	}

	users := u1.VisibleTo()
	if len(users) != 1 {
		t.Fatalf("expected foo to be visible to 1 user; got: %v", users)
	}
	if users[0].Nick != "baz" {
		t.Errorf("expected foo to be visible to baz; got: %v", users[0])
	}

	c1.receive <- irc.ParseMessage("NICK foo_")
	expectReply(t, c1, ":foo!root@client1 NICK foo_")
	expectReply(t, c2, ":foo!root@client1 NICK foo_")

	c2.receive <- irc.ParseMessage("PRIVMSG #chat :hello")
	expectReply(t, c1, ":baz!root@client2 PRIVMSG #chat :hello")
	expectEvent(t, events, ChanMsgEvent)
	// Note: baz doesn't get an echo back here
	c1.receive <- irc.ParseMessage("PRIVMSG baz :sup?")
	expectReply(t, c2, ":foo_!root@client1 PRIVMSG baz :sup?")
	expectEvent(t, events, UserMsgEvent)

	c1.receive <- irc.ParseMessage("PART #chat")
	expectReply(t, c1, ":foo_!root@client1 PART #chat")
	expectReply(t, c2, ":foo_!root@client1 PART #chat")
	expectEvent(t, events, PartEvent)
}
