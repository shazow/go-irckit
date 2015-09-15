package user

import (
	"io/ioutil"

	"github.com/alexcesaro/log"
	"github.com/alexcesaro/log/golog"
)

var logger log.Logger

func SetLogger(l log.Logger) {
	logger = l
}

func init() {
	// Set a default null logger
	SetLogger(golog.New(ioutil.Discard, log.None))
}
