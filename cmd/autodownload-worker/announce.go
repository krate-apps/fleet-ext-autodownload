package main

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	magnetRE     = regexp.MustCompile(`(?i)magnet:\?[^\s<>"']+`)
	btihRE       = regexp.MustCompile(`(?i)urn:btih:([a-fA-F0-9]{40}|[a-zA-Z2-7]{32})`)
	sizeRE       = regexp.MustCompile(`(?i)(?:^|[\s\[(])(\d+(?:\.\d+)?)\s*(GiB|GB|MiB|MB|TiB|TB)(?:$|[\s\])])`)
	torrentURLRE = regexp.MustCompile(`(?i)https?://[^\s<>"']+\.torrent(?:\?[^\s<>"']*)?`)
)

type announce struct {
	Title      string
	Magnet     string
	TorrentURL string
	InfoHash   string
	SizeMB     int // 0 = unknown
	Channel    string
	ServerID   string
}

func parseAnnounce(msg string) (announce, bool) {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return announce{}, false
	}
	magnet := magnetRE.FindString(msg)
	if magnet != "" {
		magnet = strings.TrimRight(magnet, ".,;)]}>\"'")
	}
	torrentURL := torrentURLRE.FindString(msg)
	if torrentURL != "" {
		torrentURL = strings.TrimRight(torrentURL, ".,;)]}>\"'")
	}
	if magnet == "" && torrentURL == "" {
		return announce{}, false
	}
	titleSrc := msg
	if magnet != "" {
		titleSrc = strings.Replace(titleSrc, magnet, " ", 1)
	}
	if torrentURL != "" {
		titleSrc = strings.Replace(titleSrc, torrentURL, " ", 1)
	}
	title := strings.TrimSpace(strings.Join(strings.Fields(titleSrc), " "))
	if title == "" && magnet != "" {
		title = displayNameFromMagnet(magnet)
	}
	a := announce{
		Title:      title,
		Magnet:     magnet,
		TorrentURL: torrentURL,
		InfoHash:   infoHashFromMagnet(magnet),
		SizeMB:     sizeMBFromText(msg),
	}
	return a, true
}

func displayNameFromMagnet(magnet string) string {
	lower := strings.ToLower(magnet)
	idx := strings.Index(lower, "dn=")
	if idx < 0 {
		return ""
	}
	rest := magnet[idx+3:]
	if amp := strings.IndexByte(rest, '&'); amp >= 0 {
		rest = rest[:amp]
	}
	rest = strings.ReplaceAll(rest, "+", " ")
	return strings.TrimSpace(rest)
}

func infoHashFromMagnet(magnet string) string {
	if magnet == "" {
		return ""
	}
	sub := btihRE.FindStringSubmatch(magnet)
	if len(sub) < 2 {
		return ""
	}
	return strings.ToUpper(sub[1])
}

func sizeMBFromText(text string) int {
	sub := sizeRE.FindStringSubmatch(text)
	if len(sub) < 3 {
		return 0
	}
	n, err := strconv.ParseFloat(sub[1], 64)
	if err != nil {
		return 0
	}
	unit := strings.ToUpper(sub[2])
	switch unit {
	case "TIB", "TB":
		return int(n * 1024 * 1024)
	case "GIB", "GB":
		return int(n * 1024)
	case "MIB", "MB":
		return int(n)
	default:
		return 0
	}
}
