package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

func processGrab(st settings, f filter, a announce, source string) error {
	if a.InfoHash != "" && hasGrabbed(a.InfoHash) {
		return nil
	}
	pathOrURL := a.Magnet
	if pathOrURL == "" {
		pathOrURL = a.TorrentURL
	}
	rec := grabRecord{
		InfoHash:     a.InfoHash,
		Title:        a.Title,
		MagnetOrPath: pathOrURL,
		FilterID:     f.ID,
		ServerID:     a.ServerID,
		Channel:      a.Channel,
		DryRun:       st.DryRun,
		Source:       source,
	}
	if st.DryRun {
		rec.OK = true
		return appendGrab(rec)
	}
	start := st.Start
	if f.Start != nil {
		start = *f.Start
	}

	if strings.TrimSpace(a.Magnet) != "" {
		if _, err := hostCall("fleet.torrents.add", map[string]any{
			"magnets": []string{a.Magnet},
			"start":   start,
		}); err != nil {
			rec.OK = false
			rec.Error = err.Error()
			_ = appendGrab(rec)
			return err
		}
	} else if strings.TrimSpace(a.TorrentURL) != "" {
		if err := addTorrentURL(a.TorrentURL, start); err != nil {
			rec.OK = false
			rec.Error = err.Error()
			_ = appendGrab(rec)
			return err
		}
	} else {
		rec.OK = false
		rec.Error = "no magnet or torrent URL"
		_ = appendGrab(rec)
		return fmt.Errorf("no magnet or torrent URL")
	}

	if label := strings.TrimSpace(f.Label); label != "" && a.InfoHash != "" {
		_, _ = hostCall("fleet.torrents.setLabel", map[string]any{
			"hash":  a.InfoHash,
			"label": label,
		})
	}
	rec.OK = true
	return appendGrab(rec)
}

func addTorrentURL(u string, start bool) error {
	raw, err := hostCall("fleet.http.fetch", map[string]any{"url": u})
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	var fetched struct {
		Status     int    `json:"status"`
		BodyBase64 string `json:"bodyBase64"`
	}
	if err := json.Unmarshal(raw, &fetched); err != nil {
		return fmt.Errorf("fetch decode: %w", err)
	}
	if fetched.Status >= 400 {
		return fmt.Errorf("HTTP %d", fetched.Status)
	}
	if strings.TrimSpace(fetched.BodyBase64) == "" {
		return fmt.Errorf("empty torrent body")
	}
	if _, err := hostCall("fleet.torrents.add", map[string]any{
		"torrentBase64": fetched.BodyBase64,
		"start":         start,
	}); err != nil {
		return err
	}
	return nil
}
