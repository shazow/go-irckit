package server

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/shazow/irc-news/user"
	"github.com/sorcix/irc"
)

var ErrHandshakeFailed = errors.New("handshake failed")

const serverName = "irc-news"

type Server interface {
	Join(user.User) error
	Prefix() *irc.Prefix
}

func New() Server {
	return &server{
		users: make(map[string]*user.User),
	}
}

type server struct {
	sync.Mutex
	users  map[string]*user.User
	prefix *irc.Prefix
}

func (s *server) Prefix() *irc.Prefix {
	return &irc.Prefix{Name: serverName}
}

// Join starts the handshake for user.User and returns when complete or failed.
func (s *server) Join(u user.User) error {
	err := s.handshake(u)
	if err != nil {
		return err
	}
	go s.handle(u)
	return nil
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
			identity.Changed = time.Now()
		case irc.USER:
			identity.User = msg.Params[0]
			identity.Real = msg.Trailing
			u.Set(identity)

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
