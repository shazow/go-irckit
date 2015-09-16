package server

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/shazow/irc-news/user"
	"github.com/sorcix/irc"
)

var ErrHandshakeFailed = errors.New("handshake failed")

const serverName = "irc-news"

func ID(s string) string {
	return strings.ToLower(s)
}

type Server interface {
	Join(user.User) error
	Prefix() *irc.Prefix
}

func New() Server {
	return &server{
		users:    make(map[string]user.User),
		channels: make(map[string]Channel),
		created:  time.Now(),
	}
}

type server struct {
	sync.RWMutex
	users    map[string]user.User
	channels map[string]Channel
	prefix   *irc.Prefix
	count    int
	created  time.Time
}

func (s *server) Prefix() *irc.Prefix {
	return &irc.Prefix{Name: serverName}
}

// Channel returns an existing or new channel with the give name.
func (s *server) Channel(name string) Channel {
	s.Lock()
	id := ID(name)
	ch, ok := s.channels[id]
	if !ok {
		ch = NewChannel(s, name)
		id = ch.ID()
		s.channels[id] = ch
	}
	s.Unlock()
	return ch
}

// Join starts the handshake for a new user.User and returns when complete or failed.
func (s *server) Join(u user.User) error {
	err := s.handshake(u)
	if err != nil {
		return err
	}
	go s.handle(u)
	return nil
}

func (s *server) guestNick() string {
	s.Lock()
	defer s.Unlock()

	s.count++
	return fmt.Sprintf("Guest%s", s.count)
}

func (s *server) handle(u user.User) {
	for {
		msg, err := u.Decode()
		if err != nil {
			logger.Errorf("handle error for %s: %s", u.ID(), err.Error())
			return
		}
		switch msg.Command {
		case irc.PING:
			_ = u.Encode(&irc.Message{
				Prefix:  s.Prefix(),
				Command: irc.PONG,
				Params:  msg.Params,
			})
		}
	}
}

func (s *server) add(u user.User) (ok bool) {
	s.Lock()
	defer s.Unlock()

	id := u.ID()
	if _, exists := s.users[id]; exists {
		return false
	}

	s.users[id] = u
	return true
}

func (s *server) handshake(u user.User) error {
	// Read messages until we filled in USER details.
	identity := user.Identity{}
	for i := 5; i > 0; i-- {
		// Consume 5 messages then give up.
		msg, err := u.Decode()
		if err != nil {
			return err
		}
		if len(msg.Params) < 1 {
			continue
		}
		switch msg.Command {
		case irc.NICK:
			identity.Nick = msg.Params[0]
		case irc.USER:
			identity.User = msg.Params[0]
			identity.Real = msg.Trailing
			u.Set(identity)
			ok := s.add(u)
			if !ok {
				identity.Nick = s.guestNick()
				ok = s.add(u)
			}
			if !ok {
				return ErrHandshakeFailed
			}
			return u.Encode(&irc.Message{
				Prefix:   s.Prefix(),
				Command:  irc.RPL_WELCOME,
				Params:   []string{identity.User},
				Trailing: fmt.Sprintf("Welcome!"),
			})
		}
	}
	return ErrHandshakeFailed
}
