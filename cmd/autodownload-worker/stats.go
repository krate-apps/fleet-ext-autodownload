package main

import (
	"sync"
	"time"
)

type announceEvent struct {
	At     time.Time
	Kind   string // seen | reject | grab
	Server string
}

var (
	announceMu     sync.Mutex
	announceEvents []announceEvent
)

const maxAnnounceEvents = 4000

func noteAnnounceSeen(serverID string) {
	pushAnnounceEvent("seen", serverID)
}

func noteAnnounceReject(serverID string) {
	pushAnnounceEvent("reject", serverID)
}

func noteAnnounceGrab(serverID string) {
	pushAnnounceEvent("grab", serverID)
}

func pushAnnounceEvent(kind, serverID string) {
	announceMu.Lock()
	defer announceMu.Unlock()
	announceEvents = append(announceEvents, announceEvent{
		At:     time.Now().UTC(),
		Kind:   kind,
		Server: serverID,
	})
	if len(announceEvents) > maxAnnounceEvents {
		announceEvents = announceEvents[len(announceEvents)-maxAnnounceEvents:]
	}
}

type rollingStats24h struct {
	WindowHours int `json:"windowHours"`
	Announces   int `json:"announces"`
	Rejects     int `json:"rejects"`
	Grabs       int `json:"grabs"`
}

func rollingAnnounceStats24h() rollingStats24h {
	announceMu.Lock()
	defer announceMu.Unlock()
	cut := time.Now().UTC().Add(-24 * time.Hour)
	st := rollingStats24h{WindowHours: 24}
	kept := announceEvents[:0]
	for _, e := range announceEvents {
		if e.At.Before(cut) {
			continue
		}
		kept = append(kept, e)
		switch e.Kind {
		case "seen":
			st.Announces++
		case "reject":
			st.Rejects++
		case "grab":
			st.Grabs++
		}
	}
	announceEvents = kept
	return st
}
