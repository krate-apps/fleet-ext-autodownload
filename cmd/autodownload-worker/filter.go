package main

import (
	"regexp"
	"strings"
)

func matchFilter(f filter, a announce) bool {
	ok, _ := matchFilterReason(f, a)
	return ok
}

// matchFilterReason returns whether the announce matches and a short reason
// (matched, disabled, server, channel, titleInclude, titleExclude, resolution, source, group, minSize, maxSize).
func matchFilterReason(f filter, a announce) (bool, string) {
	if !f.Enabled {
		return false, "disabled"
	}
	if len(f.ServerIDs) > 0 {
		ok := false
		sid := strings.TrimSpace(a.ServerID)
		for _, want := range f.ServerIDs {
			if strings.EqualFold(strings.TrimSpace(want), sid) {
				ok = true
				break
			}
		}
		if !ok {
			return false, "server"
		}
	}
	if len(f.Channels) > 0 {
		ok := false
		ch := normalizeChannelName(a.Channel)
		for _, want := range f.Channels {
			if normalizeChannelName(want) == ch {
				ok = true
				break
			}
		}
		if !ok {
			return false, "channel"
		}
	}
	title := a.Title
	if f.TitleInclude != "" {
		re, err := regexp.Compile("(?i)" + f.TitleInclude)
		if err != nil {
			return false, "titleInclude"
		}
		if !re.MatchString(title) {
			return false, "titleInclude"
		}
	}
	if f.TitleExclude != "" {
		re, err := regexp.Compile("(?i)" + f.TitleExclude)
		if err == nil && re.MatchString(title) {
			return false, "titleExclude"
		}
	}
	if tok := strings.TrimSpace(f.Resolution); tok != "" {
		if !strings.Contains(strings.ToLower(title), strings.ToLower(tok)) {
			return false, "resolution"
		}
	}
	if tok := strings.TrimSpace(f.Source); tok != "" {
		if !strings.Contains(strings.ToLower(title), strings.ToLower(tok)) {
			return false, "source"
		}
	}
	if tok := strings.TrimSpace(f.Group); tok != "" {
		if !strings.Contains(strings.ToLower(title), strings.ToLower(tok)) {
			return false, "group"
		}
	}
	if a.SizeMB > 0 {
		if f.MinSizeMB > 0 && a.SizeMB < f.MinSizeMB {
			return false, "minSize"
		}
		if f.MaxSizeMB > 0 && a.SizeMB > f.MaxSizeMB {
			return false, "maxSize"
		}
	}
	return true, "matched"
}

func firstMatchingFilter(filters []filter, a announce) (filter, bool) {
	for _, f := range filters {
		if matchFilter(f, a) {
			return f, true
		}
	}
	return filter{}, false
}

// explainFilters evaluates each filter and returns per-filter match + reason (PD-ADL-02).
func explainFilters(filters []filter, a announce) (matched filter, matchedOK bool, rows []map[string]any) {
	rows = make([]map[string]any, 0, len(filters))
	for _, f := range filters {
		ok, reason := matchFilterReason(f, a)
		rows = append(rows, map[string]any{
			"filterId":   f.ID,
			"filterName": f.Name,
			"matched":    ok,
			"reason":     reason,
		})
		if ok && !matchedOK {
			matched = f
			matchedOK = true
		}
	}
	return matched, matchedOK, rows
}
