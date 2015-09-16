package user

import (
	"net"
	"strings"
	"sync"

	"github.com/sorcix/irc"
)

type Identity struct {
	Nick string // From NICK command
	User string // From USER command
	Real string // From USER command
	Host string
}

func (n Identity) ID() string {
	return strings.ToLower(n.Nick)
}

func (n Identity) Prefix() *irc.Prefix {
	return &irc.Prefix{
		Name: n.Nick,
		User: n.User,
		Host: n.Host,
	}
}

type User interface {
	ID() string
	Prefix() *irc.Prefix
	Set(Identity)

	// TODO: Implement timeout
	Close() error

	// irc.Encode, irc.Decoder
	Encode(*irc.Message) error
	Decode() (*irc.Message, error)
}

func New(conn net.Conn) User {
	host := resolveHost(conn.RemoteAddr())
	return &user{
		Identity: Identity{Host: host},
		Conn:     conn,
		Encoder:  irc.NewEncoder(conn),
		Decoder:  irc.NewDecoder(conn),
	}
}

type user struct {
	net.Conn
	*irc.Encoder
	*irc.Decoder

	mu sync.Mutex
	Identity
}

func (user *user) Set(identity Identity) {
	user.mu.Lock()
	if identity.User != "" {
		user.Identity.User = identity.User
	}
	if identity.Nick != "" {
		user.Identity.Nick = identity.Nick
	}
	if identity.Real != "" {
		user.Identity.Real = identity.Real
	}
	if identity.Host != "" {
		user.Identity.Host = identity.Host
	}
	user.mu.Unlock()
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
