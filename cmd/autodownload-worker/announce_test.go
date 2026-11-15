package main

import "testing"

func TestParseAnnounce(t *testing.T) {
	msg := `NEW  Some.Show.S01E01.720p.WEB-DL-GROUP 1.2 GiB magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=Some.Show`
	a, ok := parseAnnounce(msg)
	if !ok {
		t.Fatal("expected parse ok")
	}
	if a.InfoHash != "0123456789ABCDEF0123456789ABCDEF01234567" {
		t.Fatalf("hash=%q", a.InfoHash)
	}
	if a.SizeMB < 1200 || a.SizeMB > 1300 {
		t.Fatalf("sizeMB=%d", a.SizeMB)
	}
	if a.Magnet == "" || a.Title == "" {
		t.Fatalf("%+v", a)
	}
	if _, ok := parseAnnounce("no magnet here"); ok {
		t.Fatal("expected fail")
	}
}

func TestParseAnnounceTorrentURL(t *testing.T) {
	msg := `NEW Show.720p https://tracker.example/dl/abc.torrent?pass=1`
	a, ok := parseAnnounce(msg)
	if !ok {
		t.Fatal("expected parse ok")
	}
	if a.TorrentURL != "https://tracker.example/dl/abc.torrent?pass=1" {
		t.Fatalf("url=%q", a.TorrentURL)
	}
	if a.Magnet != "" {
		t.Fatalf("unexpected magnet %q", a.Magnet)
	}
	if a.Title == "" {
		t.Fatal("expected title")
	}
}

func TestParsePRIVMSG(t *testing.T) {
	line := `:bot!a@b PRIVMSG #announce :Title magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa`
	pm, ok := parsePRIVMSG(line)
	if !ok || !containsMagnet(pm.Text) || pm.Channel != "#announce" {
		t.Fatalf("pm=%+v ok=%v", pm, ok)
	}
}

func containsMagnet(s string) bool {
	return len(s) > 0 && (len(magnetRE.FindString(s)) > 0)
}
