package server

import (
	"strings"
	"sync"

	"github.com/shazow/irc-news/user"

	"github.com/sorcix/irc"
)

type Channel interface {
	ID() string
	Join(user.User) error
	Names() []string
}

type channel struct {
	server Server
	name   string

	mu    sync.RWMutex
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

	var err error
	err = u.Encode(&irc.Message{
		Prefix:  u.Prefix(),
		Command: irc.JOIN,
		Params:  []string{ch.name},
	})
	if err != nil {
		return err
	}

	ch.mu.Lock()
	topic := ch.topic
	ch.users[u.ID()] = u
	ch.mu.Unlock()

	topicCmd := irc.RPL_TOPIC
	if topic == "" {
		topicCmd = irc.RPL_NOTOPIC
		topic = "No topic is set"
	}

	err = u.EncodeMany(
		&irc.Message{
			Prefix:   ch.server.Prefix(),
			Command:  irc.RPL_NAMREPLY,
			Params:   []string{u.Nick(), "=", ch.name},
			Trailing: strings.Join(ch.Names(), " "),
		},
		&irc.Message{
			Prefix:   ch.server.Prefix(),
			Params:   []string{u.Nick()},
			Command:  irc.RPL_ENDOFNAMES,
			Trailing: "End of /NAMES list.",
		},
		&irc.Message{
			Prefix:   ch.server.Prefix(),
			Command:  topicCmd,
			Params:   []string{ch.name},
			Trailing: topic,
		},
	)
	return err
}

func (ch channel) Names() []string {
	ch.mu.RLock()
	defer ch.mu.RUnlock()

	names := make([]string, 0, len(ch.users))
	for _, u := range ch.users {
		names = append(names, u.Nick())
	}

	return names
}
