package server

import (
	"io"
	"strings"
	"sync"

	"github.com/sorcix/irc"
)

type Channel interface {
	ID() string
	Join(*User) error
	Part(u *User, text string)
	Message(from *User, text string)
	Names() []string

	io.Closer
}

type channel struct {
	server    Server
	name      string
	keepEmpty bool // Skip removing channel when empty?

	mu    sync.RWMutex
	topic string
	users map[string]*User
}

func NewChannel(server Server, name string) Channel {
	return &channel{
		server: server,
		name:   name,
		users:  map[string]*User{},
	}
}

func (ch *channel) ID() string {
	return ID(ch.name)
}

func (ch *channel) Message(from *User, text string) {
	msg := &irc.Message{
		Prefix:   from.Prefix(),
		Command:  irc.PRIVMSG,
		Params:   []string{ch.name},
		Trailing: text,
	}
	ch.mu.RLock()
	for _, to := range ch.users {
		// TODO: Check err and kick failures?
		if to.Nick == from.Nick {
			continue
		}
		to.Encode(msg)
	}
	ch.mu.RUnlock()
}

// Leave will remove the user from the channel and emit a PART message.
func (ch *channel) Part(u *User, text string) {
	msg := &irc.Message{
		Prefix:   u.Prefix(),
		Command:  irc.PART,
		Params:   []string{ch.name},
		Trailing: text,
	}
	ch.mu.Lock()
	if _, ok := ch.users[u.ID()]; !ok {
		ch.mu.Unlock()
		u.Encode(&irc.Message{
			Prefix:   ch.server.Prefix(),
			Command:  irc.ERR_NOTONCHANNEL,
			Params:   []string{ch.name},
			Trailing: "You're not on that channel",
		})
		return
	}
	for _, to := range ch.users {
		to.Encode(msg)
	}
	delete(ch.users, u.ID())
	if !ch.keepEmpty && len(ch.users) == 0 && ch.server != nil {
		ch.server.RemoveChannel(ch.name)
		ch.server = nil
	}
	ch.mu.Unlock()
}

// Close will evict all users in the channel.
func (ch *channel) Close() error {
	ch.mu.Lock()
	for _, to := range ch.users {
		to.Encode(&irc.Message{
			Prefix:  to.Prefix(),
			Command: irc.PART,
			Params:  []string{ch.name},
		})
	}
	ch.users = map[string]*User{}
	ch.mu.Unlock()
	return nil
}

func (ch *channel) Join(u *User) error {
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

	err = u.Encode(
		&irc.Message{
			Prefix:   ch.server.Prefix(),
			Command:  irc.RPL_NAMREPLY,
			Params:   []string{u.Nick, "=", ch.name},
			Trailing: strings.Join(ch.Names(), " "),
		},
		&irc.Message{
			Prefix:   ch.server.Prefix(),
			Params:   []string{u.Nick},
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
		names = append(names, u.Nick)
	}

	return names
}
