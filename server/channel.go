package server

import (
	"sync"

	"github.com/shazow/irc-news/user"

	"github.com/sorcix/irc"
)

type Channel interface {
	ID() string
	Join(user.User) error
}

type channel struct {
	server Server
	name   string

	mu    sync.Mutex
	topic string
	users map[string]user.User
}

func NewChannel(server Server, name string) Channel {
	return &channel{
		server: server,
		name:   name,
		users:  map[string]user.User{},
	}
}

func (ch *channel) ID() string {
	return ID(ch.name)
}

func (ch *channel) Join(u user.User) error {
	// TODO: Check if user is already here?

	u.Encode(&irc.Message{
		Prefix:  u.Prefix(),
		Command: irc.JOIN,
		Params:  []string{ch.name},
	})

	ch.mu.Lock()
	topic := ch.topic
	ch.users[u.ID()] = u
	ch.mu.Unlock()

	// TODO: RPL_NOTOPIC?
	u.Encode(&irc.Message{
		Prefix:   ch.server.Prefix(),
		Command:  irc.RPL_TOPIC,
		Params:   []string{topic},
		Trailing: ch.topic,
	})

	return nil
}
