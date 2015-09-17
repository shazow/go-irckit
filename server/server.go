package server

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/sorcix/irc"
)

var ErrHandshakeFailed = errors.New("handshake failed")

const serverName = "irc-news"

func ID(s string) string {
	return strings.ToLower(s)
}

type Server interface {
	Connect(net.Conn) error
	Prefix() *irc.Prefix
	Channel(string) Channel
	RemoveChannel(string) Channel
}

func New() Server {
	return &server{
		users:    map[string]*User{},
		channels: map[string]Channel{},
		created:  time.Now(),
	}
}

type server struct {
	sync.RWMutex
	users      map[string]*User
	channels   map[string]Channel
	prefix     *irc.Prefix
	count      int
	created    time.Time
	inviteOnly bool
}

// Prefix returns the server's command prefix string.
func (s *server) Prefix() *irc.Prefix {
	return &irc.Prefix{Name: serverName}
}

// HasUser returns whether a given user is in the server.
func (s *server) HasUser(nick string) (*User, bool) {
	s.RLock()
	u, exists := s.users[ID(nick)]
	s.RUnlock()
	return u, exists
}

// HasChannel returns whether a given channel already exists.
func (s *server) HasChannel(name string) (Channel, bool) {
	s.RLock()
	ch, exists := s.channels[ID(name)]
	s.RUnlock()
	return ch, exists
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

// CloseChannel will evict members and remove from the server's storage.
func (s *server) RemoveChannel(name string) Channel {
	s.Lock()
	id := ID(name)
	ch, ok := s.channels[id]
	if !ok {
		return nil
	}
	delete(s.channels, id)
	s.Unlock()
	return ch
}

// Connect starts the handshake for a new User and returns when complete or failed.
func (s *server) Connect(conn net.Conn) error {
	u := NewUser(conn)
	err := s.handshake(u)
	if err != nil {
		u.Close()
		return err
	}
	go s.handle(u)
	return nil
}

// Leave will remove the user from all channels.
func (s *server) Leave(u *User, message string) {
	s.Lock()
	defer s.Unlock()

	delete(s.users, u.ID())
	for _, ch := range u.Channels {
		ch.Part(u, message)
	}
	u.Channels = map[string]Channel{}
}

func (s *server) guestNick() string {
	s.Lock()
	defer s.Unlock()

	s.count++
	return fmt.Sprintf("Guest%d", s.count)
}

// names lists all names for a given channel
func (s *server) names(u *User, channels ...string) []*irc.Message {
	// TODO: Support full list?
	r := []*irc.Message{}
	for _, channel := range channels {
		ch, exists := s.HasChannel(channel)
		if !exists {
			continue
		}
		msg := irc.Message{
			Prefix:   s.Prefix(),
			Command:  irc.RPL_NAMREPLY,
			Params:   []string{u.Nick, "=", channel},
			Trailing: strings.Join(ch.Names(), " "),
		}
		r = append(r, &msg)
	}
	endParams := []string{u.Nick}
	if len(channels) == 1 {
		endParams = append(endParams, channels[0])
	}
	// FIXME: Do we need to return an ENDOFNAMES for each channel when there are >1 queried?
	r = append(r, &irc.Message{
		Prefix:   s.Prefix(),
		Params:   endParams,
		Command:  irc.RPL_ENDOFNAMES,
		Trailing: "End of /NAMES list.",
	})
	return r
}

func (s *server) handle(u *User) {
	var partMsg string
	defer s.Leave(u, partMsg)

	for {
		msg, err := u.Decode()
		if err != nil {
			logger.Errorf("handle decode error for %s: %s", u.ID(), err.Error())
			return
		}
		switch msg.Command {
		case irc.PART:
			if len(msg.Params) < 1 {
				u.Encode(&irc.Message{
					Prefix:  s.Prefix(),
					Command: irc.ERR_NEEDMOREPARAMS,
					Params:  []string{msg.Command},
				})
				continue
			}
			channels := strings.Split(msg.Params[0], ",")
			for _, chName := range channels {
				ch, exists := s.HasChannel(chName)
				if !exists {
					u.Encode(&irc.Message{
						Prefix:   s.Prefix(),
						Command:  irc.ERR_NOSUCHCHANNEL,
						Params:   []string{chName},
						Trailing: "No such channel",
					})
					continue
				}
				ch.Part(u, msg.Trailing)
			}
		case irc.QUIT:
			err = u.Encode(&irc.Message{
				Prefix:   s.Prefix(),
				Command:  irc.ERROR,
				Trailing: "You will be missed.",
			})
			partMsg = "quit"
			return
		case irc.PING:
			err = u.Encode(&irc.Message{
				Prefix:  s.Prefix(),
				Command: irc.PONG,
				Params:  msg.Params,
			})
		case irc.JOIN:
			if len(msg.Params) < 1 {
				u.Encode(&irc.Message{
					Prefix:  s.Prefix(),
					Command: irc.ERR_NEEDMOREPARAMS,
					Params:  []string{msg.Command},
				})
			} else if s.inviteOnly {
				err = u.Encode(&irc.Message{
					Prefix:   s.Prefix(),
					Command:  irc.ERR_INVITEONLYCHAN,
					Trailing: "Cannot join channel (+i)",
				})
			} else {
				channel := msg.Params[0]
				s.Channel(channel).Join(u)
			}
		case irc.NAMES:
			if len(msg.Params) < 1 {
				u.Encode(&irc.Message{
					Prefix:  s.Prefix(),
					Command: irc.ERR_NEEDMOREPARAMS,
					Params:  []string{msg.Command},
				})
				continue
			}
			err = u.Encode(s.names(u, msg.Params[0])...)
		case irc.PRIVMSG:
			if len(msg.Params) < 1 {
				u.Encode(&irc.Message{
					Prefix:  s.Prefix(),
					Command: irc.ERR_NEEDMOREPARAMS,
					Params:  []string{msg.Command},
				})
				continue
			}
			query := msg.Params[0]
			if toChan, exists := s.HasChannel(query); exists {
				toChan.Message(u, msg.Trailing)
			} else if toUser, exists := s.HasUser(query); exists {
				toUser.Encode(&irc.Message{
					Prefix:   u.Prefix(),
					Command:  irc.PRIVMSG,
					Params:   []string{u.Nick},
					Trailing: msg.Trailing,
				})
			} else {
				err = u.Encode(&irc.Message{
					Prefix:   s.Prefix(),
					Command:  irc.ERR_NOSUCHNICK,
					Params:   msg.Params,
					Trailing: "No such nick/channel",
				})
			}
		}
		if err != nil {
			logger.Errorf("handle encode error for %s: %s", u.ID(), err.Error())
			return
		}
	}
}

func (s *server) add(u *User) (ok bool) {
	s.Lock()
	defer s.Unlock()

	id := u.ID()
	if _, exists := s.users[id]; exists {
		return false
	}

	s.users[id] = u
	return true
}

func (s *server) handshake(u *User) error {
	// Read messages until we filled in USER details.
	for i := 5; i > 0; i-- {
		// Consume 5 messages then give up.
		msg, err := u.Decode()
		if err != nil {
			return err
		}

		if len(msg.Params) < 1 {
			u.Encode(&irc.Message{
				Prefix:  s.Prefix(),
				Command: irc.ERR_NEEDMOREPARAMS,
				Params:  []string{msg.Command},
			})
		}

		switch msg.Command {
		case irc.NICK:
			u.Nick = msg.Params[0]
		case irc.USER:
			u.User = msg.Params[0]
			u.Real = msg.Trailing
			if u.Nick == "" {
				u.Nick = u.User
			}
		}

		if u.Nick == "" || u.User == "" {
			// Wait for both to be set before proceeding
			continue
		}

		ok := s.add(u)
		if !ok {
			u.Encode(
				&irc.Message{
					Prefix:   s.Prefix(),
					Command:  irc.ERR_NICKNAMEINUSE,
					Params:   []string{u.Nick},
					Trailing: "Nickname is already in use",
				},
			)
			continue
		}

		return u.Encode(
			&irc.Message{
				Prefix:   s.Prefix(),
				Command:  irc.RPL_WELCOME,
				Params:   []string{u.Nick},
				Trailing: fmt.Sprintf("Welcome!"),
			},
		)
	}
	return ErrHandshakeFailed
}
