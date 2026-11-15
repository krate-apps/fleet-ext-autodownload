package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

type settings struct {
	Enabled        bool        `json:"enabled"`
	Paused         bool        `json:"paused"`
	DryRun         bool        `json:"dryRun"`
	IntervalSec    int         `json:"intervalSec"`
	Start          bool        `json:"start"`
	RemoveAfterAdd bool        `json:"removeAfterAdd"`
	GrabRetention  int         `json:"grabRetention"` // max journal entries; 0 = default 500
	Dirs           []string    `json:"dirs"`
	IRCServers     []ircServer `json:"ircServers"`
	Filters        []filter    `json:"filters"`
}

type ircServer struct {
	ID               string   `json:"id"`
	Host             string   `json:"host"`
	Port             int      `json:"port"`
	TLS              bool     `json:"tls"`
	Nick             string   `json:"nick"`
	Password         string   `json:"password,omitempty"`
	NickServPassword string   `json:"nickServPassword,omitempty"`
	Channels         []string `json:"channels"`
	Enabled          bool     `json:"enabled"`
}

type filter struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Enabled      bool     `json:"enabled"`
	TitleInclude string   `json:"titleInclude"`
	TitleExclude string   `json:"titleExclude"`
	MinSizeMB    int      `json:"minSizeMB,omitempty"`
	MaxSizeMB    int      `json:"maxSizeMB,omitempty"`
	Resolution   string   `json:"resolution,omitempty"`
	Source       string   `json:"source,omitempty"`
	Group        string   `json:"group,omitempty"`
	Label        string   `json:"label,omitempty"`
	Start        *bool    `json:"start,omitempty"`
	Channels     []string `json:"channels,omitempty"`
	ServerIDs    []string `json:"serverIds,omitempty"`
}

func defaultSettings() settings {
	return settings{
		Enabled:        false,
		Paused:         false,
		DryRun:         false,
		IntervalSec:    30,
		Start:          true,
		RemoveAfterAdd: true,
		GrabRetention:  defaultGrabRetention,
		Dirs:           []string{},
		IRCServers:     []ircServer{},
		Filters:        []filter{},
	}
}

func parseDirs(text string) []string {
	lines := make([]string, 0)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lines = append(lines, line)
	}
	return normalizeDirs(lines)
}

func normalizeDirs(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]struct{}{}
	for _, line := range in {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if _, ok := seen[line]; ok {
			continue
		}
		seen[line] = struct{}{}
		out = append(out, line)
	}
	return out
}

func normalizeIRCServers(in []ircServer) ([]ircServer, error) {
	out := make([]ircServer, 0, len(in))
	seen := map[string]struct{}{}
	for i, s := range in {
		s.ID = strings.TrimSpace(s.ID)
		s.Host = strings.TrimSpace(s.Host)
		s.Nick = strings.TrimSpace(s.Nick)
		if s.ID == "" {
			return nil, fmt.Errorf("server[%d]: id required", i)
		}
		if s.Host == "" {
			return nil, fmt.Errorf("server %s: host required", s.ID)
		}
		if s.Port <= 0 {
			s.Port = 6667
		}
		if s.Nick == "" {
			s.Nick = "fleet"
		}
		chans := make([]string, 0, len(s.Channels))
		for _, ch := range s.Channels {
			ch = normalizeChannelName(ch)
			if ch != "" {
				chans = append(chans, ch)
			}
		}
		s.Channels = chans
		if _, ok := seen[s.ID]; ok {
			return nil, fmt.Errorf("duplicate server id %q", s.ID)
		}
		seen[s.ID] = struct{}{}
		out = append(out, s)
	}
	return out, nil
}

func normalizeFilters(in []filter) []filter {
	out := make([]filter, 0, len(in))
	for i, f := range in {
		f.ID = strings.TrimSpace(f.ID)
		f.Name = strings.TrimSpace(f.Name)
		if f.ID == "" {
			f.ID = "f" + strings.ReplaceAll(f.Name, " ", "_")
			if f.ID == "f" {
				f.ID = "f" + itoa(i+1)
			}
		}
		if f.Name == "" {
			f.Name = f.ID
		}
		for j := range f.Channels {
			f.Channels[j] = normalizeChannelName(f.Channels[j])
		}
		out = append(out, f)
	}
	return out
}

func parseChannelsCSV(text string) []string {
	out := make([]string, 0)
	for _, part := range strings.Split(text, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if !strings.HasPrefix(part, "#") {
			part = "#" + part
		}
		out = append(out, part)
	}
	return out
}

func normalizeChannelName(ch string) string {
	ch = strings.TrimSpace(ch)
	if ch == "" {
		return ""
	}
	if !strings.HasPrefix(ch, "#") {
		ch = "#" + ch
	}
	return strings.ToLower(ch)
}

// parseIRCServersText lines: id|host|port|tls|nick|pass|nickserv|channels|on
func parseIRCServersText(text string) ([]ircServer, error) {
	out := make([]ircServer, 0)
	seen := map[string]struct{}{}
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "|", 9)
		if len(parts) < 8 {
			return nil, fmt.Errorf("irc line %d: expected id|host|port|tls|nick|pass|nickserv|channels|on", i+1)
		}
		id := strings.TrimSpace(parts[0])
		host := strings.TrimSpace(parts[1])
		if id == "" || host == "" {
			return nil, fmt.Errorf("irc line %d: id and host required", i+1)
		}
		key := strings.ToLower(id)
		if _, dup := seen[key]; dup {
			return nil, fmt.Errorf("irc line %d: duplicate id %q", i+1, id)
		}
		seen[key] = struct{}{}
		port, _ := strconv.Atoi(strings.TrimSpace(parts[2]))
		tlsOn := parseOnOff(parts[3], true)
		nick := strings.TrimSpace(parts[4])
		pass := parts[5]
		nickserv := parts[6]
		channels := parseChannelsCSV(parts[7])
		enabled := true
		if len(parts) >= 9 {
			enabled = parseOnOff(parts[8], true)
		}
		out = append(out, ircServer{
			ID:               id,
			Host:             host,
			Port:             port,
			TLS:              tlsOn,
			Nick:             nick,
			Password:         pass,
			NickServPassword: nickserv,
			Channels:         channels,
			Enabled:          enabled,
		})
	}
	return out, nil
}

func parseOnOff(s string, defaultOn bool) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "on", "1", "true", "yes":
		return true
	case "off", "0", "false", "no":
		return false
	default:
		return defaultOn
	}
}

func ircServersText(servers []ircServer) string {
	if len(servers) == 0 {
		return ""
	}
	lines := make([]string, 0, len(servers))
	for _, s := range servers {
		tlsFlag := "off"
		if s.TLS {
			tlsFlag = "on"
		}
		enFlag := "off"
		if s.Enabled {
			enFlag = "on"
		}
		lines = append(lines, strings.Join([]string{
			s.ID,
			s.Host,
			itoa(s.Port),
			tlsFlag,
			s.Nick,
			s.Password,
			s.NickServPassword,
			strings.Join(s.Channels, ","),
			enFlag,
		}, "|"))
	}
	return strings.Join(lines, "\n")
}

func parseFiltersJSONLines(text string) ([]filter, error) {
	out := make([]filter, 0)
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var f filter
		if err := json.Unmarshal([]byte(line), &f); err != nil {
			return nil, err
		}
		if f.ID == "" {
			f.ID = "f" + strings.ReplaceAll(strings.TrimSpace(f.Name), " ", "_")
			if f.ID == "f" {
				f.ID = "f" + itoa(i+1)
			}
		}
		if f.Name == "" {
			f.Name = f.ID
		}
		for j := range f.Channels {
			f.Channels[j] = normalizeChannelName(f.Channels[j])
		}
		out = append(out, f)
	}
	return out, nil
}

func filtersJSONLines(filters []filter) string {
	if len(filters) == 0 {
		return ""
	}
	var b strings.Builder
	for i, f := range filters {
		raw, _ := json.Marshal(f)
		if i > 0 {
			b.WriteByte('\n')
		}
		b.Write(raw)
	}
	return b.String()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [16]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func loadSettings() (settings, error) {
	raw, err := hostCall("fleet.storage.get", map[string]any{"key": "settings"})
	if err != nil {
		return settings{}, err
	}
	if string(raw) == "null" || len(raw) == 0 {
		return defaultSettings(), nil
	}
	var st settings
	if err := json.Unmarshal(raw, &st); err != nil {
		return defaultSettings(), nil
	}
	if st.IntervalSec == 0 {
		st.IntervalSec = 30
	}
	if st.GrabRetention <= 0 {
		st.GrabRetention = defaultGrabRetention
	}
	if st.GrabRetention > maxGrabRetention {
		st.GrabRetention = maxGrabRetention
	}
	if st.Dirs == nil {
		st.Dirs = []string{}
	}
	if st.IRCServers == nil {
		st.IRCServers = []ircServer{}
	}
	if st.Filters == nil {
		st.Filters = []filter{}
	}
	return st, nil
}

func saveSettings(st *settings) error {
	_, err := hostCall("fleet.storage.put", map[string]any{"key": "settings", "value": st})
	return err
}
