package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

type autodlExport struct {
	Schema  int         `json:"schema"`
	Version string      `json:"version,omitempty"`
	Servers []ircServer `json:"ircServers"`
	Filters []filter    `json:"filters"`
}

func exportConfig(includeSecrets bool) (autodlExport, error) {
	st, err := loadSettings()
	if err != nil {
		return autodlExport{}, err
	}
	servers := make([]ircServer, 0, len(st.IRCServers))
	for _, s := range st.IRCServers {
		cp := s
		if !includeSecrets {
			cp.Password = ""
			cp.NickServPassword = ""
		}
		servers = append(servers, cp)
	}
	filters := append([]filter(nil), st.Filters...)
	return autodlExport{
		Schema:  1,
		Version: "org.fleet.autodownload",
		Servers: servers,
		Filters: filters,
	}, nil
}

func importConfig(raw json.RawMessage, merge bool) (map[string]any, error) {
	var payload autodlExport
	if err := json.Unmarshal(raw, &payload); err != nil {
		// allow bare {ircServers,filters}
		var loose struct {
			IRCServers []ircServer `json:"ircServers"`
			Filters    []filter    `json:"filters"`
		}
		if err2 := json.Unmarshal(raw, &loose); err2 != nil {
			return nil, fmt.Errorf("invalid export JSON")
		}
		payload.Servers = loose.IRCServers
		payload.Filters = loose.Filters
	}
	servers, err := normalizeIRCServers(payload.Servers)
	if err != nil {
		return nil, fmt.Errorf("ircServers: %w", err)
	}
	filters := normalizeFilters(payload.Filters)

	st, err := loadSettings()
	if err != nil {
		return nil, err
	}
	if merge {
		byID := map[string]ircServer{}
		for _, s := range st.IRCServers {
			byID[strings.ToLower(s.ID)] = s
		}
		for _, s := range servers {
			key := strings.ToLower(s.ID)
			if prev, ok := byID[key]; ok {
				if s.Password == "" {
					s.Password = prev.Password
				}
				if s.NickServPassword == "" {
					s.NickServPassword = prev.NickServPassword
				}
			}
			byID[key] = s
		}
		merged := make([]ircServer, 0, len(byID))
		for _, s := range byID {
			merged = append(merged, s)
		}
		st.IRCServers = merged

		fByID := map[string]filter{}
		for _, f := range st.Filters {
			fByID[strings.ToLower(f.ID)] = f
		}
		for _, f := range filters {
			fByID[strings.ToLower(f.ID)] = f
		}
		mergedF := make([]filter, 0, len(fByID))
		for _, f := range fByID {
			mergedF = append(mergedF, f)
		}
		st.Filters = mergedF
	} else {
		// Replace servers/filters but keep secrets when import strips them and id matches.
		if len(servers) > 0 {
			prevByID := map[string]ircServer{}
			for _, s := range st.IRCServers {
				prevByID[strings.ToLower(s.ID)] = s
			}
			for i := range servers {
				key := strings.ToLower(servers[i].ID)
				if prev, ok := prevByID[key]; ok {
					if servers[i].Password == "" {
						servers[i].Password = prev.Password
					}
					if servers[i].NickServPassword == "" {
						servers[i].NickServPassword = prev.NickServPassword
					}
				}
			}
			st.IRCServers = servers
		}
		if payload.Filters != nil || len(filters) > 0 {
			st.Filters = filters
		}
	}
	if err := saveSettings(&st); err != nil {
		return nil, err
	}
	go restartIRCLoops()
	return map[string]any{
		"ok":      true,
		"merge":   merge,
		"irc":     len(st.IRCServers),
		"filters": len(st.Filters),
	}, nil
}
