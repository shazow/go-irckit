package irckit

import (
	"net"
	"strings"
	"sync"

	"github.com/sorcix/irc"
)

// NewUser creates a *User, wrapping a connection with metadata we need for our server.
func NewUser(c Conn) *User {
	return &User{
		Conn:     c,
		Host:     "*",
		Channels: map[Channel]struct{}{},
	}
}

// NewUserNet creates a *User from a net.Conn connection.
func NewUserNet(c net.Conn) *User {
	return NewUser(&conn{
		Conn:    c,
		Encoder: irc.NewEncoder(c),
		Decoder: irc.NewDecoder(c),
	})
}

type User struct {
	Conn

	sync.RWMutex
	Nick string // From NICK command
	User string // From USER command
	Real string // From USER command
	Host string

	Channels map[Channel]struct{}
}

func (u *User) ID() string {
	return strings.ToLower(u.Nick)
}

func (u *User) Prefix() *irc.Prefix {
	return &irc.Prefix{
		Name: u.Nick,
		User: u.User,
		Host: u.Host,
	}
}

func (u *User) ForSeen(fn func(*User) error) error {
	seen := map[*User]struct{}{}
	seen[u] = struct{}{}
	for ch := range u.Channels {
		err := ch.ForUser(func(other *User) error {
			if _, dupe := seen[other]; dupe {
				return nil
			}
			seen[other] = struct{}{}
			return fn(other)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// EncodeMany calls Encode for each msg until an err occurs, then returns
func (user *User) Encode(msgs ...*irc.Message) (err error) {
	for _, msg := range msgs {
		logger.Debugf("-> %s", msg)
		err := user.Conn.Encode(msg)
		if err != nil {
			return err
		}
	}
	return nil
}

func (user *User) Decode() (*irc.Message, error) {
	msg, err := user.Conn.Decode()
	logger.Debugf("<- %s", msg)
	return msg, err
}
