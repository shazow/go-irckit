package main

import (
	"net"

	"github.com/sorcix/irc"
)

type Client interface {
	Handshake() error
}

func NewClient(conn net.Conn) Client {
	return &client{
		Conn:    conn,
		Encoder: irc.NewEncoder(conn),
		Decoder: irc.NewDecoder(conn),
	}
}

type client struct {
	net.Conn
	*irc.Encoder
	*irc.Decoder
}

func (client *client) Handshake() error {
	// TODO: Fill this in
	return nil
}
