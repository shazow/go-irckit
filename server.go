package main

type Server interface {
	Join(Client) error
}

func NewServer() Server {
	return &server{}
}

type server struct{}

func (s *server) Join(client Client) error {
	// TODO: Fill this in
	return nil
}
