package irckit

import "errors"

var ErrInvalidMode = errors.New("invalid mode string")

type Modes map[rune]struct{}

const SetMode = '+'
const UnsetMode = '-'

func (m Modes) Check(mode rune) bool {
	if m == nil {
		return false
	}
	_, ok := m[mode]
	return ok
}

func (m Modes) set(mode rune) {
	m[mode] = struct{}{}
}

func (m Modes) unset(mode rune) {
	delete(m, mode)
}

func (mPtr *Modes) Parse(s string) {
	if len(s) == 0 {
		return nil
	}
	if *mPtr == nil {
		*mPtr = Modes{}
	}
	m := *mPtr
	do := m.set
	for _, mode := range s {
		switch mode {
		case SetMode:
			do = m.set
		case UnsetMode:
			do = m.unset
		default:
			do(mode)
		}
	}

	return nil
}
