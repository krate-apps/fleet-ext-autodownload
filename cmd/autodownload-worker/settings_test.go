package main

import "testing"

func TestNormalizeDirs(t *testing.T) {
	got := normalizeDirs([]string{" /a ", "", "/a", "#skip", "/b"})
	if len(got) != 2 || got[0] != "/a" || got[1] != "/b" {
		t.Fatalf("got %#v", got)
	}
}

func TestNormalizeIRCServers(t *testing.T) {
	got, err := normalizeIRCServers([]ircServer{
		{ID: "main", Host: "irc.example", Channels: []string{"announce", "#x"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Port != 6667 || got[0].Nick != "fleet" {
		t.Fatalf("defaults: %#v", got[0])
	}
	if len(got[0].Channels) != 2 || got[0].Channels[0] != "#announce" {
		t.Fatalf("channels: %#v", got[0].Channels)
	}
	_, err = normalizeIRCServers([]ircServer{{ID: "a", Host: "h"}, {ID: "a", Host: "h2"}})
	if err == nil {
		t.Fatal("expected duplicate id error")
	}
}

func TestNormalizeFilters(t *testing.T) {
	got := normalizeFilters([]filter{{Name: "720", Channels: []string{"announce"}}})
	if got[0].ID == "" || got[0].Name != "720" || got[0].Channels[0] != "#announce" {
		t.Fatalf("%#v", got[0])
	}
}
