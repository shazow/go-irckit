package user

import (
	"net"
	"time"

	"github.com/sorcix/irc"
)

type Name struct {
	Nick    string // From NICK command
	User    string // From USER command
	Real    string // From USER command
	Changed time.Time
}

type User interface {
	ID() string
	Name() Name
	SetName(Name)

	// TODO: Implement timeout
	Close() error

	// irc.Encode, irc.Decoder
	Encode(*irc.Message) (err error)
	Decode() (*irc.Message, error)
}

func New(conn net.Conn) User {
	return &user{
		Conn:    conn,
		Encoder: irc.NewEncoder(conn),
		Decoder: irc.NewDecoder(conn),
	}
}

type user struct {
	net.Conn
	*irc.Encoder
	*irc.Decoder
	name Name
}

func (user *user) Encode(msg *irc.Message) (err error) {
	logger.Debugf("-> %s", msg)
	return user.Encoder.Encode(msg)
}

func (user *user) Decode() (*irc.Message, error) {
	msg, err := user.Decoder.Decode()
	logger.Debugf("<- %s", msg)
	return msg, err
}

func (user *user) ID() string {
	return user.name.Nick
}

func (user *user) Name() Name {
	return user.name
}

func (user *user) SetName(name Name) {
	name.Changed = time.Now()
	user.name = name
}
