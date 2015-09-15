package main

import (
	"net"

	"github.com/shazow/irc-news/server"
	"github.com/shazow/irc-news/user"
)

type Host interface {
	Start(net.Listener) // Listen and accept connection, blocking.
}

func NewHost() Host {
	return &host{
		server: server.New(),
	}
}

type host struct {
	server server.Server
}

func (h *host) Start(listener net.Listener) {
	for {
		conn, err := listener.Accept()

		if err != nil {
			logger.Errorf("Failed to accept connection: %v", err)
			return
		}

		// Goroutineify to resume accepting sockets early
		go func() {
			u := user.New(conn)
			err = h.server.Join(u)
			if err != nil {
				logger.Errorf("Failed to join: %v", err)
				return
			}
		}()
	}
}
