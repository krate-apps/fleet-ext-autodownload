package main

import "testing"

func TestFilterGrabs(t *testing.T) {
	list := []grabRecord{
		{TS: "1", Title: "Alpha Show", ServerID: "main", Channel: "#a", FilterID: "f1", Source: "irc", OK: true},
		{TS: "2", Title: "Beta Movie", ServerID: "spare", Channel: "#b", FilterID: "f2", Source: "watch", OK: false, Error: "x"},
		{TS: "3", Title: "Gamma", ServerID: "main", Channel: "a", FilterID: "f1", Source: "irc", DryRun: true, OK: true},
	}
	got := filterGrabs(list, grabQuery{ServerID: "main"})
	if len(got) != 2 {
		t.Fatalf("server filter: %d", len(got))
	}
	got = filterGrabs(list, grabQuery{Channel: "a"})
	if len(got) != 2 {
		t.Fatalf("channel normalize: %d", len(got))
	}
	got = filterGrabs(list, grabQuery{Result: "fail"})
	if len(got) != 1 || got[0].Title != "Beta Movie" {
		t.Fatalf("fail: %+v", got)
	}
	got = filterGrabs(list, grabQuery{Result: "dry"})
	if len(got) != 1 || !got[0].DryRun {
		t.Fatalf("dry: %+v", got)
	}
	got = filterGrabs(list, grabQuery{Query: "alpha"})
	if len(got) != 1 {
		t.Fatalf("query: %+v", got)
	}
	got = filterGrabs(list, grabQuery{Source: "watch"})
	if len(got) != 1 {
		t.Fatalf("source: %+v", got)
	}
}

func TestMatchFilterReason(t *testing.T) {
	a := announce{Title: "Movie.2024.720p.BluRay-GROUP", SizeMB: 8000, Channel: "#a", ServerID: "main"}
	f := filter{Enabled: true, TitleInclude: `.*720p.*`, Resolution: "720p", Group: "GROUP"}
	ok, reason := matchFilterReason(f, a)
	if !ok || reason != "matched" {
		t.Fatalf("ok=%v reason=%s", ok, reason)
	}
	f.TitleExclude = "720p"
	ok, reason = matchFilterReason(f, a)
	if ok || reason != "titleExclude" {
		t.Fatalf("ok=%v reason=%s", ok, reason)
	}
	f.TitleExclude = ""
	f.Channels = []string{"#other"}
	ok, reason = matchFilterReason(f, a)
	if ok || reason != "channel" {
		t.Fatalf("ok=%v reason=%s", ok, reason)
	}
}
