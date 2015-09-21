package irckit

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/sorcix/irc"
)

var ErrHandshakeFailed = errors.New("handshake failed")

// ID will normalize a name to be used as a unique identifier for comparison.
func ID(s string) string {
	return strings.ToLower(s)
}

type Server interface {
	// Close disconnects everyone.
	Close() error

	// Prefix returns the prefix string sent by the server for server-origin messages.
	Prefix() *irc.Prefix

	// Connect starts the handshake for a new user, blocks until it's completed or failed with an error.
	Connect(*User) error

	// Quit removes the user from all the channels and disconnects.
	Quit(*User, string)

	// HasUser returns an existing User with a given Nick.
	HasUser(string) (*User, bool)

	// RenameUser changes the Nick of a User if the new name is available.
	RenameUser(*User, string) bool

	// Channel gets or creates a new channel with the given name.
	Channel(string) Channel

	// HasChannel returns an existing Channel with a given name.
	HasChannel(string) (Channel, bool)

	// RemoveChannel removes the channel with a given name and returns it if it existed.
	// TODO: Return bool too? Or change HasChannel/HasUser to match this return style.
	RemoveChannel(string) Channel

	Publisher
}

// NewServer creates a server with a given name.
func NewServer(name string) Server {
	return &server{
		name:      name,
		users:     map[string]*User{},
		channels:  map[string]Channel{},
		created:   time.Now(),
		Publisher: SyncPublisher(),
	}
}

type server struct {
	created    time.Time
	name       string
	inviteOnly bool

	sync.RWMutex
	count    int
	users    map[string]*User
	channels map[string]Channel

	Publisher
}

func (s *server) Close() error {
	// TODO: Send notice or something?
	// TODO: Clear channels?
	s.Lock()
	for _, u := range s.users {
		u.Close()
	}
	s.Unlock()
	return nil
}

// Prefix returns the server's command prefix string.
func (s *server) Prefix() *irc.Prefix {
	return &irc.Prefix{Name: s.name}
}

// HasUser returns whether a given user is in the server.
func (s *server) HasUser(nick string) (*User, bool) {
	s.RLock()
	u, exists := s.users[ID(nick)]
	s.RUnlock()
	return u, exists
}

// Rename will attempt to rename the given user's Nick if it's available.
func (s *server) RenameUser(u *User, newNick string) bool {
	s.Lock()
	if _, exists := s.users[ID(newNick)]; exists {
		s.Unlock()
		u.Encode(&irc.Message{
			Prefix:   s.Prefix(),
			Command:  irc.ERR_NICKNAMEINUSE,
			Params:   []string{newNick},
			Trailing: "Nickname is already in use",
		})
		return false
	}

	delete(s.users, u.ID())
	oldPrefix := u.Prefix()
	u.Nick = newNick
	s.users[u.ID()] = u
	s.Unlock()

	changeMsg := &irc.Message{
		Prefix:  oldPrefix,
		Command: irc.NICK,
		Params:  []string{newNick},
	}
	u.Encode(changeMsg)
	for _, other := range u.VisibleTo() {
		other.Encode(changeMsg)
	}
	return true
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
func (s *server) Connect(u *User) error {
	err := s.handshake(u)
	if err != nil {
		u.Close()
		return err
	}
	go s.handle(u)
	s.Publish(&event{ConnectEvent, s, nil, u, nil})
	return nil
}

// Quit will remove the user from all channels and disconnect.
// TODO: Rename to Quit
func (s *server) Quit(u *User, message string) {
	s.Lock()
	defer s.Unlock()

	delete(s.users, u.ID())
	for ch := range u.channels {
		ch.Part(u, message)
	}
	u.channels = map[Channel]struct{}{}
	u.Close()
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
		// FIXME: This needs to be broken up into multiple messages to fit <510 chars
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
	defer s.Quit(u, partMsg)

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
				s.Publish(&event{PartEvent, s, ch, u, msg})
			}
		case irc.QUIT:
			partMsg = msg.Trailing
			u.Encode(&irc.Message{
				Prefix:   u.Prefix(),
				Command:  irc.QUIT,
				Trailing: partMsg,
			})
			u.Encode(&irc.Message{
				Prefix:   s.Prefix(),
				Command:  irc.ERROR,
				Trailing: "You will be missed.",
			})
			s.Publish(&event{QuitEvent, s, nil, u, msg})
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
				ch := s.Channel(channel)
				err = ch.Join(u)
				if err == nil {
					s.Publish(&event{JoinEvent, s, ch, u, msg})
				}
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
				s.Publish(&event{ChanMsgEvent, s, toChan, u, msg})
			} else if toUser, exists := s.HasUser(query); exists {
				toUser.Encode(&irc.Message{
					Prefix:   u.Prefix(),
					Command:  irc.PRIVMSG,
					Params:   []string{toUser.Nick},
					Trailing: msg.Trailing,
				})
				s.Publish(&event{UserMsgEvent, s, nil, u, msg})
			} else {
				err = u.Encode(&irc.Message{
					Prefix:   s.Prefix(),
					Command:  irc.ERR_NOSUCHNICK,
					Params:   msg.Params,
					Trailing: "No such nick/channel",
				})
			}
		case irc.NICK:
			if len(msg.Params) < 1 {
				u.Encode(&irc.Message{
					Prefix:  s.Prefix(),
					Command: irc.ERR_NEEDMOREPARAMS,
					Params:  []string{msg.Command},
				})
				continue
			}
			s.RenameUser(u, msg.Params[0])
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
	// Assign host
	u.Host = u.ResolveHost()

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
			continue
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
