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

const handshakeMsgTolerance = 5

// ID will normalize a name to be used as a unique identifier for comparison.
func ID(s string) string {
	return strings.ToLower(s)
}

type Prefixer interface {
	// Prefix returns a prefix configuration for the origin of the message.
	Prefix() *irc.Prefix
}

type Server interface {
	Prefixer
	Publisher

	// Connect starts the handshake for a new user, blocks until it's completed or failed with an error.
	Connect(*User) error

	// Quit removes the user from all the channels and disconnects.
	Quit(*User, string)

	// HasUser returns an existing User with a given Nick.
	HasUser(string) (*User, bool)

	// RenameUser changes the Nick of a User if the new name is available.
	// Returns whether the rename was was successful.
	RenameUser(*User, string) bool

	// Channel gets or creates a new channel with the given name.
	Channel(string) Channel

	// HasChannel returns an existing Channel with a given name.
	HasChannel(string) (Channel, bool)

	// UnlinkChannel removes the channel from the server's storage if it
	// exists. Once removed, the server is free to create a fresh channel with
	// the same ID. The server is not responsible for evicting members of an
	// unlinked channel.
	UnlinkChannel(Channel)
}

// ServerConfig produces a Server setup with configuration options.
type ServerConfig struct {
	// Name is used as the prefix for the server.
	Name string
	// InviteOnly prevents regular users from joining and making new channels.
	InviteOnly bool
	// Publisher to use. If nil, a new SyncPublisher will be used.
	Publisher Publisher
	// DiscardEmpty setting will start a goroutine to discard empty channels.
	DiscardEmpty bool
	// NewChannel overrides the constructor for a new Channel in a given Server and Name.
	NewChannel func(s Server, name string) Channel
}

func (c ServerConfig) Server() Server {
	publisher := c.Publisher
	if publisher == nil {
		publisher = SyncPublisher()
	}

	srv := &server{
		config:    c,
		users:     map[string]*User{},
		channels:  map[string]Channel{},
		created:   time.Now(),
		Publisher: publisher,
	}
	if c.DiscardEmpty {
		srv.channelEvents = make(chan Event, 1)
		go srv.cleanupEmpty()
	}

	return srv
}

// NewServer creates a server.
func NewServer(name string) Server {
	return ServerConfig{Name: name}.Server()
}

type server struct {
	config  ServerConfig
	created time.Time

	sync.RWMutex
	count         int
	users         map[string]*User
	channels      map[string]Channel
	channelEvents chan Event

	Publisher
}

func (s *server) Close() error {
	// TODO: Send notice or something?
	// TODO: Clear channels?
	s.Lock()
	for _, u := range s.users {
		u.Close()
	}
	s.Publisher.Close()
	s.Unlock()
	return nil
}

// Prefix returns the server's command prefix string.
func (s *server) Prefix() *irc.Prefix {
	return &irc.Prefix{Name: s.config.Name}
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
		newFn := NewChannel
		if s.config.NewChannel != nil {
			newFn = s.config.NewChannel
		}
		ch = newFn(s, name)
		id = ch.ID()
		s.channels[id] = ch
		s.Unlock()
		if s.config.DiscardEmpty {
			ch.Subscribe(s.channelEvents)
		}
		s.Publish(&event{NewChanEvent, s, ch, nil, nil})
	} else {
		s.Unlock()
	}
	return ch
}

// cleanupEmpty receives Channel candidates for cleaning up and removes them if they're empty. (Blocking)
func (s *server) cleanupEmpty() {
	for evt := range s.channelEvents {
		if evt.Kind() != EmptyChanEvent {
			continue
		}
		ch := evt.Channel()
		s.Lock()
		if s.channels[ch.ID()] != ch {
			// Not the same channel anymore, already been replaced.
			s.Unlock()
			continue
		}
		if ch.Len() != 0 {
			// Not empty.
			s.Unlock()
			continue
		}
		delete(s.channels, ch.ID())
		s.Unlock()
	}
}

// UnlinkChannel unlinks the channel from the server's storage, returns whether it existed.
func (s *server) UnlinkChannel(ch Channel) {
	s.Lock()
	chStored := s.channels[ch.ID()]
	r := chStored == ch
	if r {
		delete(s.channels, ch.ID())
	}
	s.Unlock()
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
func (s *server) Quit(u *User, message string) {
	go u.Close()
	s.Lock()
	delete(s.users, u.ID())
	s.Unlock()
}

func (s *server) guestNick() string {
	s.Lock()
	defer s.Unlock()

	s.count++
	return fmt.Sprintf("Guest%d", s.count)
}

func (s *server) who(u *User, mask string, op bool) []*irc.Message {
	endMsg := &irc.Message{
		Prefix:   s.Prefix(),
		Params:   []string{u.Nick, mask},
		Command:  irc.RPL_ENDOFWHO,
		Trailing: "End of /WHO list.",
	}

	// TODO: Handle arbitrary masks, not just channels
	ch, exists := s.HasChannel(mask)
	if !exists {
		return []*irc.Message{endMsg}
	}

	r := make([]*irc.Message, 0, ch.Len()+1)
	for _, other := range ch.Users() {
		// <me> <channel> <user> <host> <server> <nick> [H/G]: 0 <real>
		r = append(r, &irc.Message{
			Prefix:   s.Prefix(),
			Params:   []string{u.Nick, mask, other.User, other.Host, "*", other.Nick, "H"},
			Command:  irc.RPL_WHOREPLY,
			Trailing: "0 " + other.Real,
		})
	}

	r = append(r, endMsg)
	return r
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
		if msg == nil {
			// Ignore empty messages
			continue
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
				Prefix:   s.Prefix(),
				Command:  irc.PONG,
				Params:   []string{s.config.Name},
				Trailing: msg.Trailing,
			})
		case irc.JOIN:
			if len(msg.Params) < 1 {
				u.Encode(&irc.Message{
					Prefix:  s.Prefix(),
					Command: irc.ERR_NEEDMOREPARAMS,
					Params:  []string{msg.Command},
				})
			} else if s.config.InviteOnly {
				err = u.Encode(&irc.Message{
					Prefix:   s.Prefix(),
					Command:  irc.ERR_INVITEONLYCHAN,
					Trailing: "Cannot join channel (+i)",
				})
			} else {
				channels := strings.Split(msg.Params[0], ",")
				for _, channel := range channels {
					ch := s.Channel(channel)
					err = ch.Join(u)
					if err == nil {
						s.Publish(&event{JoinEvent, s, ch, u, msg})
					}
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
		case irc.WHO:
			if len(msg.Params) < 1 {
				u.Encode(&irc.Message{
					Prefix:  s.Prefix(),
					Command: irc.ERR_NEEDMOREPARAMS,
					Params:  []string{msg.Command},
				})
				continue
			}
			opFilter := len(msg.Params) >= 2 && msg.Params[1] == "o"
			err = u.Encode(s.who(u, msg.Params[0], opFilter)...)
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
	for i := handshakeMsgTolerance; i > 0; i-- {
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
				Trailing: fmt.Sprintf("Welcome! %s", u.Prefix()),
			},
		)
	}
	return ErrHandshakeFailed
}
