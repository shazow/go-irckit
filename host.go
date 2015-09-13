package main

import "net"

type Host interface {
	Start(net.Listener) // Listen and accept connection, blocking.
}

func NewHost() Host {
	return &host{
		server: NewServer(),
	}
}

type host struct {
	server Server
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
			client := NewClient(conn)

			err := client.Handshake()
			if err != nil {
				logger.Errorf("Failed to handshake: %v", err)
				return
			}
			err = h.server.Join(client)
			if err != nil {
				logger.Errorf("Failed to join: %v", err)
				return
			}
		}()
	}
}
