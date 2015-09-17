[![GoDoc](https://godoc.org/github.com/shazow/go-irckit?status.svg)](https://godoc.org/github.com/shazow/go-irckit)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](https://raw.githubusercontent.com/shazow/go-irckit/master/LICENSE)
[![Build Status](https://travis-ci.org/shazow/go-irckit.svg?branch=master)](https://travis-ci.org/shazow/go-irckit)

# go-irckit

Minimal IRC server (and maybe client) toolkit in Go.

Built to experiment with the IRC protocol, mostly implementing the RFCs from
scratch so don't expect it to be super-robust or correct. Aimed to be a good
foundation for bootstrapping quick proof-of-concepts on the IRC protocol.

**Status**: `v0.0` (no stability guarantee); if you're using this, please open an
issue with your API requirements.

Check the project's [upcoming milestones](https://github.com/shazow/go-irckit/milestones)
to get a feel for what's prioritized.


## Details

- Parsing and encoding is done by https://github.com/sorcix/irc
- Server implementation references:
  [rfc1459](https://tools.ietf.org/html/rfc1459),
  **[rfc2812](https://tools.ietf.org/html/rfc2812)**,
  [rfc2813](https://tools.ietf.org/html/rfc2813).

## License

MIT
