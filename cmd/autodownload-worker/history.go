package main

import (
	"encoding/json"
	"strings"
	"sync"
	"time"
)

const (
	defaultGrabRetention = 500
	maxGrabRetention     = 5000
)

type grabRecord struct {
	TS           string `json:"ts"`
	InfoHash     string `json:"infohash"`
	Title        string `json:"title"`
	MagnetOrPath string `json:"magnetOrPath"`
	FilterID     string `json:"filterId"`
	ServerID     string `json:"serverId,omitempty"`
	Channel      string `json:"channel,omitempty"`
	DryRun       bool   `json:"dryRun"`
	OK           bool   `json:"ok"`
	Error        string `json:"error,omitempty"`
	Source       string `json:"source,omitempty"` // irc | watch
}

type grabQuery struct {
	Limit    int
	ServerID string
	Channel  string
	FilterID string
	Source   string
	Result   string // ok | fail | dry | any/empty
	Query    string // substring title/infohash
}

type grabStats24h struct {
	WindowHours int `json:"windowHours"`
	Total       int `json:"total"`
	OK          int `json:"ok"`
	Fail        int `json:"fail"`
	DryRun      int `json:"dryRun"`
	IRC         int `json:"irc"`
	Watch       int `json:"watch"`
}

var (
	grabMu sync.Mutex
)

func grabRetentionLimit() int {
	st, err := loadSettings()
	if err != nil {
		return defaultGrabRetention
	}
	n := st.GrabRetention
	if n <= 0 {
		n = defaultGrabRetention
	}
	if n > maxGrabRetention {
		n = maxGrabRetention
	}
	return n
}

func loadGrabs() ([]grabRecord, error) {
	raw, err := hostCall("fleet.storage.get", map[string]any{"key": "grabs"})
	if err != nil {
		return nil, err
	}
	if string(raw) == "null" || len(raw) == 0 {
		return nil, nil
	}
	var out []grabRecord
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, nil
	}
	return out, nil
}

func saveGrabs(list []grabRecord) error {
	max := grabRetentionLimit()
	if len(list) > max {
		list = list[len(list)-max:]
	}
	_, err := hostCall("fleet.storage.put", map[string]any{"key": "grabs", "value": list})
	return err
}

func hasGrabbed(infohash string) bool {
	if infohash == "" {
		return false
	}
	grabMu.Lock()
	defer grabMu.Unlock()
	list, err := loadGrabs()
	if err != nil {
		return false
	}
	for _, g := range list {
		if g.OK && strings.EqualFold(g.InfoHash, infohash) {
			return true
		}
	}
	return false
}

func appendGrab(g grabRecord) error {
	grabMu.Lock()
	defer grabMu.Unlock()
	list, err := loadGrabs()
	if err != nil {
		list = nil
	}
	if g.TS == "" {
		g.TS = time.Now().UTC().Format(time.RFC3339)
	}
	list = append(list, g)
	return saveGrabs(list)
}

func filterGrabs(list []grabRecord, q grabQuery) []grabRecord {
	out := make([]grabRecord, 0, len(list))
	wantServer := strings.TrimSpace(q.ServerID)
	wantChannel := normalizeChannelName(q.Channel)
	wantFilter := strings.TrimSpace(q.FilterID)
	wantSource := strings.ToLower(strings.TrimSpace(q.Source))
	wantResult := strings.ToLower(strings.TrimSpace(q.Result))
	needle := strings.ToLower(strings.TrimSpace(q.Query))
	for _, g := range list {
		if wantServer != "" && !strings.EqualFold(strings.TrimSpace(g.ServerID), wantServer) {
			continue
		}
		if wantChannel != "" && normalizeChannelName(g.Channel) != wantChannel {
			continue
		}
		if wantFilter != "" && !strings.EqualFold(strings.TrimSpace(g.FilterID), wantFilter) {
			continue
		}
		if wantSource != "" && !strings.EqualFold(strings.TrimSpace(g.Source), wantSource) {
			continue
		}
		switch wantResult {
		case "", "any":
		case "dry":
			if !g.DryRun {
				continue
			}
		case "ok":
			if !g.OK || g.DryRun {
				continue
			}
		case "fail":
			if g.OK {
				continue
			}
		}
		if needle != "" {
			title := strings.ToLower(g.Title)
			hash := strings.ToLower(g.InfoHash)
			if !strings.Contains(title, needle) && !strings.Contains(hash, needle) {
				continue
			}
		}
		out = append(out, g)
	}
	return out
}

func listGrabsFiltered(q grabQuery) ([]grabRecord, error) {
	grabMu.Lock()
	defer grabMu.Unlock()
	list, err := loadGrabs()
	if err != nil {
		return nil, err
	}
	filtered := filterGrabs(list, q)
	limit := q.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > len(filtered) {
		return filtered, nil
	}
	return filtered[len(filtered)-limit:], nil
}

func statsGrabs24h() (grabStats24h, error) {
	grabMu.Lock()
	defer grabMu.Unlock()
	list, err := loadGrabs()
	if err != nil {
		return grabStats24h{}, err
	}
	cut := time.Now().UTC().Add(-24 * time.Hour)
	st := grabStats24h{WindowHours: 24}
	for _, g := range list {
		ts, err := time.Parse(time.RFC3339, g.TS)
		if err != nil || ts.Before(cut) {
			continue
		}
		st.Total++
		if g.DryRun {
			st.DryRun++
		} else if g.OK {
			st.OK++
		} else {
			st.Fail++
		}
		switch strings.ToLower(strings.TrimSpace(g.Source)) {
		case "irc":
			st.IRC++
		case "watch":
			st.Watch++
		}
	}
	return st, nil
}

func clearGrabs() error {
	grabMu.Lock()
	defer grabMu.Unlock()
	return saveGrabs(nil)
}

// listGrabsLimited keeps the unfiltered last-N path used by older UI/actions.
func listGrabsLimited(limit int) ([]grabRecord, error) {
	return listGrabsFiltered(grabQuery{Limit: limit})
}
