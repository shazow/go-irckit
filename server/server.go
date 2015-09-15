package server

import (
	"errors"
	"sync"
	"time"

	"github.com/shazow/irc-news/user"
	"github.com/sorcix/irc"
)

var ErrHandshakeFailed = errors.New("handshake failed")

type Server interface {
	Join(user.User) error
}

func New() Server {
	return &server{
		users: make(map[string]*user.User),
	}
}

type server struct {
	sync.Mutex
	users map[string]*user.User
}

// Join starts the handshake for user.User and returns when complete or failed.
func (s *server) Join(u user.User) error {
	err := s.handshake(u)
	if err != nil {
		return err
	}
	return nil
}

func (s *server) handshake(u user.User) error {
	// Read messages until we filled in USER details.
	name := user.Name{}
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
			name.Nick = msg.Params[0]
			name.Changed = time.Now()
		case irc.USER:
			name.User = msg.Params[0]
			name.Real = msg.Trailing
			u.SetName(name)
			return nil
		}
	}
	return ErrHandshakeFailed
}
