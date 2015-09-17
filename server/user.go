package server

import (
	"net"
	"strings"
	"sync"

	"github.com/sorcix/irc"
)

func NewUser(conn net.Conn) *User {
	return &User{
		Conn:    conn,
		Encoder: irc.NewEncoder(conn),
		Decoder: irc.NewDecoder(conn),
	}
}

type User struct {
	net.Conn
	*irc.Encoder
	*irc.Decoder

	sync.RWMutex
	Nick string // From NICK command
	User string // From USER command
	Real string // From USER command
	Host string

	Channels map[string]Channel
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

// EncodeMany calls Encode for each msg until an err occurs, then returns
func (user *User) Encode(msgs ...*irc.Message) (err error) {
	for _, msg := range msgs {
		logger.Debugf("-> %s", msg)
		err := user.Encoder.Encode(msg)
		if err != nil {
			return err
		}
	}
	return nil
}

func (user *User) Decode() (*irc.Message, error) {
	msg, err := user.Decoder.Decode()
	logger.Debugf("<- %s", msg)
	return msg, err
}

// resolveHost will convert an IP to a Hostname, but fall back to IP on error.
func resolveHost(addr net.Addr) string {
	s := addr.String()
	ip, _, err := net.SplitHostPort(s)
	if err != nil {
		return s
	}

	names, err := net.LookupAddr(ip)
	if err != nil {
		return ip
	}

	return strings.TrimSuffix(names[0], ".")
}
