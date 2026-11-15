package main

import "testing"

func TestMatchFilter(t *testing.T) {
	a := announce{Title: "Movie.2024.720p.BluRay-GROUP", SizeMB: 8000}
	f := filter{Enabled: true, TitleInclude: `.*720p.*`, Resolution: "720p", Group: "GROUP"}
	if !matchFilter(f, a) {
		t.Fatal("expected match")
	}
	f.TitleExclude = "720p"
	if matchFilter(f, a) {
		t.Fatal("exclude should block")
	}
	f.TitleExclude = ""
	f.MinSizeMB = 9000
	if matchFilter(f, a) {
		t.Fatal("min size should block")
	}
	f.MinSizeMB = 0
	f.Enabled = false
	if matchFilter(f, a) {
		t.Fatal("disabled")
	}
}

func TestMatchFilterScope(t *testing.T) {
	a := announce{Title: "Show.720p", Channel: "#announce", ServerID: "main"}
	f := filter{Enabled: true, TitleInclude: `720p`, Channels: []string{"announce"}, ServerIDs: []string{"main"}}
	if !matchFilter(f, a) {
		t.Fatal("expected scoped match")
	}
	f.Channels = []string{"#other"}
	if matchFilter(f, a) {
		t.Fatal("channel mismatch")
	}
	f.Channels = nil
	f.ServerIDs = []string{"other"}
	if matchFilter(f, a) {
		t.Fatal("server mismatch")
	}
}

func TestFirstMatchingFilter(t *testing.T) {
	a := announce{Title: "Show.1080p"}
	filters := []filter{
		{ID: "a", Enabled: true, TitleInclude: `720p`},
		{ID: "b", Enabled: true, TitleInclude: `1080p`, Label: "tv"},
	}
	f, ok := firstMatchingFilter(filters, a)
	if !ok || f.ID != "b" || f.Label != "tv" {
		t.Fatalf("%+v ok=%v", f, ok)
	}
}

func TestParseFiltersJSONLines(t *testing.T) {
	text := `{"id":"f1","name":"hd","enabled":true,"titleInclude":".*720p.*"}
# comment
{"enabled":true,"titleInclude":"WEB","name":"web","channels":["announce"]}
`
	list, err := parseFiltersJSONLines(text)
	if err != nil || len(list) != 2 {
		t.Fatalf("list=%v err=%v", list, err)
	}
	if list[0].ID != "f1" || list[1].Name != "web" {
		t.Fatalf("%+v", list)
	}
	if len(list[1].Channels) != 1 || list[1].Channels[0] != "#announce" {
		t.Fatalf("channels=%v", list[1].Channels)
	}
}

func TestParseIRCServersText(t *testing.T) {
	text := `main|irc.example|6697|on|fleet||secret|#a,#b|on
spare|irc2.example|6667|off|bot|||#x|off
`
	list, err := parseIRCServersText(text)
	if err != nil || len(list) != 2 {
		t.Fatalf("list=%v err=%v", list, err)
	}
	if list[0].ID != "main" || !list[0].TLS || list[0].NickServPassword != "secret" {
		t.Fatalf("%+v", list[0])
	}
	if list[1].Enabled || list[1].TLS {
		t.Fatalf("%+v", list[1])
	}
	round := parseMust(ircServersText(list))
	if len(round) != 2 || round[0].NickServPassword != "secret" {
		t.Fatalf("%+v", round)
	}
}

func parseMust(text string) []ircServer {
	list, err := parseIRCServersText(text)
	if err != nil {
		panic(err)
	}
	return list
}
