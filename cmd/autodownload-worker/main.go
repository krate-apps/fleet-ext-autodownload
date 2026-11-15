package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	ID      any             `json:"id"`
}

var (
	mu       sync.Mutex
	nextHost = int64(1)
	pending  = map[int64]chan hostReply{}
	outMu    sync.Mutex
)

type hostReply struct {
	result json.RawMessage
	err    string
}

func main() {
	go loopScan()
	go func() {
		time.Sleep(2 * time.Second)
		restartIRCLoops()
	}()

	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 0, 64*1024), 8<<20)
	for in.Scan() {
		line := append([]byte(nil), in.Bytes()...)
		if len(line) == 0 {
			continue
		}
		var probe map[string]json.RawMessage
		if err := json.Unmarshal(line, &probe); err != nil {
			continue
		}
		if _, hasMethod := probe["method"]; !hasMethod {
			var resp struct {
				ID     any             `json:"id"`
				Result json.RawMessage `json:"result"`
				Error  *struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal(line, &resp); err != nil {
				continue
			}
			idNum, ok := asInt64(resp.ID)
			if !ok {
				continue
			}
			mu.Lock()
			ch := pending[idNum]
			delete(pending, idNum)
			mu.Unlock()
			if ch == nil {
				continue
			}
			msg := ""
			if resp.Error != nil {
				msg = resp.Error.Message
			}
			ch <- hostReply{result: resp.Result, err: msg}
			continue
		}

		var req request
		if err := json.Unmarshal(line, &req); err != nil {
			continue
		}
		// Handle concurrently so hostCall reverse-RPC can complete while
		// this stdin loop keeps reading host replies.
		go handleRequest(req)
	}
}

func handleRequest(req request) {
	switch req.Method {
	case "fleet.handshake", "fleet.ping", "ext.activate", "ext.health", "ext.upgrade":
		reply(req.ID, map[string]any{"ok": true})
	case "ext.deactivate":
		reply(req.ID, map[string]any{"ok": true})
	case "fleet.shutdown":
		stopIRCLoops()
		reply(req.ID, map[string]any{"ok": true})
		os.Exit(0)
	case "ext.autodownload.getSettings":
		handleGetSettings(req.ID)
	case "ext.autodownload.saveSettings":
		handleSaveSettings(req.ID, req.Params)
	case "ext.autodownload.scan":
		res, err := scanOnce()
		if err != nil {
			replyErr(req.ID, err.Error())
			return
		}
		reply(req.ID, res)
	case "ext.autodownload.listGrabs":
		var p struct {
			Limit    int    `json:"limit"`
			ServerID string `json:"serverId"`
			Channel  string `json:"channel"`
			FilterID string `json:"filterId"`
			Source   string `json:"source"`
			Result   string `json:"result"`
			Query    string `json:"query"`
		}
		_ = json.Unmarshal(req.Params, &p)
		list, err := listGrabsFiltered(grabQuery{
			Limit:    p.Limit,
			ServerID: p.ServerID,
			Channel:  p.Channel,
			FilterID: p.FilterID,
			Source:   p.Source,
			Result:   p.Result,
			Query:    p.Query,
		})
		if err != nil {
			replyErr(req.ID, err.Error())
			return
		}
		if list == nil {
			list = []grabRecord{}
		}
		reply(req.ID, map[string]any{"grabs": list})
	case "ext.autodownload.clearGrabs":
		if err := clearGrabs(); err != nil {
			replyErr(req.ID, err.Error())
			return
		}
		reply(req.ID, map[string]any{"ok": true})
	case "ext.autodownload.testFilter":
		handleTestFilter(req.ID, req.Params)
	case "ext.autodownload.exportConfig":
		var p struct {
			IncludeSecrets bool `json:"includeSecrets"`
		}
		_ = json.Unmarshal(req.Params, &p)
		exp, err := exportConfig(p.IncludeSecrets)
		if err != nil {
			replyErr(req.ID, err.Error())
			return
		}
		reply(req.ID, exp)
	case "ext.autodownload.importConfig":
		var p struct {
			Config json.RawMessage `json:"config"`
			JSON   string          `json:"json"`
			Merge  bool            `json:"merge"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			replyErr(req.ID, "invalid params")
			return
		}
		raw := p.Config
		if len(raw) == 0 && strings.TrimSpace(p.JSON) != "" {
			raw = json.RawMessage(p.JSON)
		}
		if len(raw) == 0 {
			replyErr(req.ID, "config or json required")
			return
		}
		res, err := importConfig(raw, p.Merge)
		if err != nil {
			replyErr(req.ID, err.Error())
			return
		}
		reply(req.ID, res)
	case "ext.autodownload.stats":
		grabStats, err := statsGrabs24h()
		if err != nil {
			replyErr(req.ID, err.Error())
			return
		}
		reply(req.ID, map[string]any{
			"grabs":     grabStats,
			"announces": rollingAnnounceStats24h(),
		})
	case "ext.autodownload.ircStatus":
		reply(req.ID, map[string]any{"servers": getIRCStatus()})
	case "ext.autodownload.restartIRC":
		go restartIRCLoops()
		reply(req.ID, map[string]any{"ok": true})
	default:
		replyErr(req.ID, fmt.Sprintf("method not found: %s", req.Method))
	}
}

func handleTestFilter(id any, params json.RawMessage) {
	var p struct {
		FilterID string `json:"filterId"`
		Title    string `json:"title"`
		Channel  string `json:"channel"`
		ServerID string `json:"serverId"`
		SizeMB   int    `json:"sizeMB"`
		Magnet   string `json:"magnet"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		replyErr(id, "invalid params")
		return
	}
	title := strings.TrimSpace(p.Title)
	if title == "" {
		replyErr(id, "title required")
		return
	}
	st, err := loadSettings()
	if err != nil {
		replyErr(id, err.Error())
		return
	}
	a := announce{
		Title:    title,
		Channel:  p.Channel,
		ServerID: p.ServerID,
		SizeMB:   p.SizeMB,
		Magnet:   p.Magnet,
	}
	if a.Magnet == "" {
		a.Magnet = "magnet:?xt=urn:btih:0000000000000000000000000000000000000000&dn=test"
	}
	filters := st.Filters
	if fid := strings.TrimSpace(p.FilterID); fid != "" {
		found := false
		for _, f := range st.Filters {
			if strings.EqualFold(f.ID, fid) {
				filters = []filter{f}
				found = true
				break
			}
		}
		if !found {
			replyErr(id, "filter not found: "+fid)
			return
		}
	}
	matched, matchedOK, rows := explainFilters(filters, a)
	out := map[string]any{
		"matched": matchedOK,
		"filters": rows,
		"announce": map[string]any{
			"title":    a.Title,
			"channel":  a.Channel,
			"serverId": a.ServerID,
			"sizeMB":   a.SizeMB,
		},
	}
	if matchedOK {
		out["filterId"] = matched.ID
		out["filterName"] = matched.Name
		out["reason"] = "matched"
	} else if len(rows) == 1 {
		out["reason"] = rows[0]["reason"]
	} else if len(rows) == 0 {
		out["reason"] = "noFilters"
	} else {
		out["reason"] = "noMatch"
	}
	reply(id, out)
}

func handleGetSettings(id any) {
	st, err := loadSettings()
	if err != nil {
		replyErr(id, err.Error())
		return
	}
	reply(id, map[string]any{
		"enabled":        st.Enabled,
		"paused":         st.Paused,
		"dryRun":         st.DryRun,
		"intervalSec":    st.IntervalSec,
		"start":          st.Start,
		"removeAfterAdd": st.RemoveAfterAdd,
		"grabRetention":  st.GrabRetention,
		"dirs":           st.Dirs,
		"dirsText":       strings.Join(st.Dirs, "\n"),
		"ircServersText": ircServersText(st.IRCServers),
		"filtersText":    filtersJSONLines(st.Filters),
		"filters":        st.Filters,
		"ircServers":     st.IRCServers,
	})
}

func handleSaveSettings(id any, params json.RawMessage) {
	var p struct {
		Enabled        bool         `json:"enabled"`
		Paused         bool         `json:"paused"`
		DryRun         bool         `json:"dryRun"`
		IntervalSec    int          `json:"intervalSec"`
		Start          bool         `json:"start"`
		RemoveAfterAdd bool         `json:"removeAfterAdd"`
		GrabRetention  int          `json:"grabRetention"`
		Dirs           *[]string    `json:"dirs"`
		IRCServers     *[]ircServer `json:"ircServers"`
		Filters        *[]filter    `json:"filters"`
		DirsText       string       `json:"dirsText"`
		IRCServersText string       `json:"ircServersText"`
		FiltersText    string       `json:"filtersText"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		replyErr(id, "invalid settings")
		return
	}

	var dirs []string
	if p.Dirs != nil {
		dirs = normalizeDirs(*p.Dirs)
	} else {
		dirs = parseDirs(p.DirsText)
	}

	var servers []ircServer
	if p.IRCServers != nil {
		var err error
		servers, err = normalizeIRCServers(*p.IRCServers)
		if err != nil {
			replyErr(id, "ircServers: "+err.Error())
			return
		}
	} else {
		var err error
		servers, err = parseIRCServersText(p.IRCServersText)
		if err != nil {
			replyErr(id, "ircServersText: "+err.Error())
			return
		}
	}

	var filters []filter
	if p.Filters != nil {
		filters = normalizeFilters(*p.Filters)
	} else {
		var err error
		filters, err = parseFiltersJSONLines(p.FiltersText)
		if err != nil {
			replyErr(id, "filtersText: each non-empty line must be JSON filter object")
			return
		}
	}

	st := settings{
		Enabled:        p.Enabled,
		Paused:         p.Paused,
		DryRun:         p.DryRun,
		IntervalSec:    p.IntervalSec,
		Start:          p.Start,
		RemoveAfterAdd: p.RemoveAfterAdd,
		GrabRetention:  p.GrabRetention,
		Dirs:           dirs,
		IRCServers:     servers,
		Filters:        filters,
	}
	if st.IntervalSec < 5 || st.IntervalSec > 3600 {
		replyErr(id, "intervalSec must be 5..3600")
		return
	}
	if st.GrabRetention <= 0 {
		st.GrabRetention = defaultGrabRetention
	}
	if st.GrabRetention > maxGrabRetention {
		replyErr(id, fmt.Sprintf("grabRetention must be 1..%d", maxGrabRetention))
		return
	}
	if err := saveSettings(&st); err != nil {
		replyErr(id, err.Error())
		return
	}
	go restartIRCLoops()
	reply(id, map[string]any{
		"ok":      true,
		"dirs":    len(st.Dirs),
		"filters": len(st.Filters),
		"irc":     len(st.IRCServers),
	})
}

func loopScan() {
	time.Sleep(5 * time.Second)
	for {
		st, err := loadSettings()
		interval := 30
		if err == nil && st.IntervalSec > 0 {
			interval = st.IntervalSec
		}
		if err == nil && st.Enabled && !st.Paused {
			_, _ = scanOnce()
		}
		time.Sleep(time.Duration(interval) * time.Second)
	}
}

func scanOnce() (map[string]any, error) {
	st, err := loadSettings()
	if err != nil {
		return nil, err
	}
	if st.Paused {
		return map[string]any{"ok": true, "seen": 0, "added": 0, "paused": true}, nil
	}
	added := 0
	seen := 0
	for _, dir := range st.Dirs {
		raw, err := hostCall("fleet.files.list", map[string]any{"path": dir})
		if err != nil {
			continue
		}
		var listing struct {
			Entries []struct {
				Name  string `json:"name"`
				Path  string `json:"path"`
				IsDir bool   `json:"isDir"`
			} `json:"entries"`
		}
		if err := json.Unmarshal(raw, &listing); err != nil {
			continue
		}
		for _, e := range listing.Entries {
			if e.IsDir {
				continue
			}
			ext := strings.ToLower(filepath.Ext(e.Name))
			if ext != ".torrent" && ext != ".magnet" {
				continue
			}
			seen++
			if ext == ".magnet" {
				textRaw, err := hostCall("fleet.files.readText", map[string]any{"path": e.Path})
				if err != nil {
					continue
				}
				var tr struct {
					Text string `json:"text"`
				}
				if err := json.Unmarshal(textRaw, &tr); err != nil {
					continue
				}
				magnet := strings.TrimSpace(tr.Text)
				if !strings.HasPrefix(strings.ToLower(magnet), "magnet:") {
					continue
				}
				a, ok := parseAnnounce(magnet)
				if !ok {
					a = announce{Title: e.Name, Magnet: magnet, InfoHash: infoHashFromMagnet(magnet)}
				}
				f := filter{ID: "watch", Name: "watch", Enabled: true}
				if st.DryRun {
					_ = appendGrab(grabRecord{
						InfoHash: a.InfoHash, Title: a.Title, MagnetOrPath: e.Path,
						FilterID: "watch", DryRun: true, OK: true, Source: "watch",
					})
					continue
				}
				if a.InfoHash != "" && hasGrabbed(a.InfoHash) {
					if st.RemoveAfterAdd {
						_, _ = hostCall("fleet.files.remove", map[string]any{"path": e.Path, "recursive": false})
					}
					continue
				}
				start := st.Start
				if _, err := hostCall("fleet.torrents.add", map[string]any{"magnets": []string{magnet}, "start": start}); err != nil {
					_ = appendGrab(grabRecord{
						InfoHash: a.InfoHash, Title: a.Title, MagnetOrPath: e.Path,
						FilterID: f.ID, OK: false, Error: err.Error(), Source: "watch",
					})
					continue
				}
				_ = appendGrab(grabRecord{
					InfoHash: a.InfoHash, Title: a.Title, MagnetOrPath: e.Path,
					FilterID: f.ID, OK: true, Source: "watch",
				})
				added++
			} else {
				b64raw, err := hostCall("fleet.files.readBase64", map[string]any{"path": e.Path})
				if err != nil {
					continue
				}
				var br struct {
					DataURL string `json:"dataUrl"`
					Data    string `json:"data"`
				}
				if err := json.Unmarshal(b64raw, &br); err != nil {
					continue
				}
				payload := br.Data
				if payload == "" && strings.Contains(br.DataURL, ",") {
					payload = strings.SplitN(br.DataURL, ",", 2)[1]
				}
				if payload == "" {
					continue
				}
				if st.DryRun {
					_ = appendGrab(grabRecord{
						Title: e.Name, MagnetOrPath: e.Path,
						FilterID: "watch", DryRun: true, OK: true, Source: "watch",
					})
					continue
				}
				start := st.Start
				if _, err := hostCall("fleet.torrents.add", map[string]any{"torrentBase64": payload, "start": start}); err != nil {
					_ = appendGrab(grabRecord{
						Title: e.Name, MagnetOrPath: e.Path,
						FilterID: "watch", OK: false, Error: err.Error(), Source: "watch",
					})
					continue
				}
				_ = appendGrab(grabRecord{
					Title: e.Name, MagnetOrPath: e.Path,
					FilterID: "watch", OK: true, Source: "watch",
				})
				added++
			}
			if st.RemoveAfterAdd && !st.DryRun {
				_, _ = hostCall("fleet.files.remove", map[string]any{"path": e.Path, "recursive": false})
			}
		}
	}
	return map[string]any{"ok": true, "seen": seen, "added": added}, nil
}

func hostCall(method string, params any) (json.RawMessage, error) {
	ch := make(chan hostReply, 1)
	mu.Lock()
	id := nextHost
	nextHost++
	pending[id] = ch
	mu.Unlock()

	b, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
		"id":      id,
	})
	if err != nil {
		mu.Lock()
		delete(pending, id)
		mu.Unlock()
		return nil, err
	}
	outMu.Lock()
	fmt.Fprintf(os.Stdout, "%s\n", b)
	outMu.Unlock()

	select {
	case reply := <-ch:
		if reply.err != "" {
			return nil, fmt.Errorf("%s", reply.err)
		}
		if len(reply.result) == 0 {
			return json.RawMessage("null"), nil
		}
		return reply.result, nil
	case <-time.After(60 * time.Second):
		mu.Lock()
		delete(pending, id)
		mu.Unlock()
		return nil, fmt.Errorf("host call timeout")
	}
}

func reply(id any, result any) {
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "result": result, "id": id})
	outMu.Lock()
	fmt.Fprintf(os.Stdout, "%s\n", b)
	outMu.Unlock()
}

func replyErr(id any, msg string) {
	b, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"error":   map[string]any{"code": -32000, "message": msg},
		"id":      id,
	})
	outMu.Lock()
	fmt.Fprintf(os.Stdout, "%s\n", b)
	outMu.Unlock()
}

func asInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case int64:
		return n, true
	case int:
		return int64(n), true
	case json.Number:
		i, err := n.Int64()
		return i, err == nil
	default:
		return 0, false
	}
}
