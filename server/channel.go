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
	ForUser(func(*User) error) error

	io.Closer
}

type channel struct {
	server    Server
	name      string
	keepEmpty bool // Skip removing channel when empty?

	mu       sync.RWMutex
	topic    string
	usersIdx map[*User]struct{}
}

func NewChannel(server Server, name string) Channel {
	return &channel{
		server:   server,
		name:     name,
		usersIdx: map[*User]struct{}{},
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
	for to := range ch.usersIdx {
		// TODO: Check err and kick failures?
		if to == from {
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
	if _, ok := ch.usersIdx[u]; !ok {
		ch.mu.Unlock()
		u.Encode(&irc.Message{
			Prefix:   ch.server.Prefix(),
			Command:  irc.ERR_NOTONCHANNEL,
			Params:   []string{ch.name},
			Trailing: "You're not on that channel",
		})
		return
	}
	for to := range ch.usersIdx {
		to.Encode(msg)
	}
	delete(ch.usersIdx, u)
	if !ch.keepEmpty && len(ch.usersIdx) == 0 && ch.server != nil {
		ch.server.RemoveChannel(ch.name)
		ch.server = nil
	}
	ch.mu.Unlock()
}

// Close will evict all users in the channel.
func (ch *channel) Close() error {
	ch.mu.Lock()
	for to := range ch.usersIdx {
		to.Encode(&irc.Message{
			Prefix:  to.Prefix(),
			Command: irc.PART,
			Params:  []string{ch.name},
		})
	}
	ch.usersIdx = map[*User]struct{}{}
	ch.mu.Unlock()
	return nil
}

func (ch *channel) ForUser(fn func(*User) error) error {
	for u := range ch.usersIdx {
		err := fn(u)
		if err != nil {
			return err
		}
	}
	return nil
}

func (ch *channel) Join(u *User) error {
	// TODO: Check if user is already here?

	ch.mu.Lock()
	if _, exists := ch.usersIdx[u]; exists {
		ch.mu.Unlock()
		return nil
	}
	topic := ch.topic
	ch.usersIdx[u] = struct{}{}
	ch.mu.Unlock()

	msg := &irc.Message{
		Prefix:  u.Prefix(),
		Command: irc.JOIN,
		Params:  []string{ch.name},
	}
	for to := range ch.usersIdx {
		to.Encode(msg)
	}

	topicCmd := irc.RPL_TOPIC
	if topic == "" {
		topicCmd = irc.RPL_NOTOPIC
		topic = "No topic is set"
	}

	err := u.Encode(
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

	names := make([]string, 0, len(ch.usersIdx))
	for u := range ch.usersIdx {
		names = append(names, u.Nick)
	}

	return names
}
