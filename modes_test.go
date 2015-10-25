package irckit

import "testing"

func TestHistory(t *testing.T) {
	var m Modes = nil
	if got, want := m.Check('a'), false; got != want {
		t.Errorf("got %q; want %q", got, want)
	}

	if err := m.Parse("+abc-de-b"); err != nil {
		t.Errorf("error: %s", err)
	}

	if got, want := m.Check('a'), true; got != want {
		t.Errorf("got %q; want %q", got, want)
	}
	if got, want := m.Check('b'), false; got != want {
		t.Errorf("got %q; want %q", got, want)
	}
	if got, want := m.Check('d'), false; got != want {
		t.Errorf("got %q; want %q", got, want)
	}
}
