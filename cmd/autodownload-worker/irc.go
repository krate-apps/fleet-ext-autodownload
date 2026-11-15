package main

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

type ircStatusEntry struct {
	ID                string `json:"id"`
	Host              string `json:"host"`
	OK                bool   `json:"ok"`
	Detail            string `json:"detail"`
	Enabled           bool   `json:"enabled"`
	LastAnnounceAt    string `json:"lastAnnounceAt,omitempty"`
	LastAnnounceTitle string `json:"lastAnnounceTitle,omitempty"`
	LastAnnounceChan  string `json:"lastAnnounceChannel,omitempty"`
	LastError         string `json:"lastError,omitempty"`
}

type privmsg struct {
	Channel string
	Text    string
}

var (
	ircStatusMu sync.Mutex
	ircStatus   = map[string]ircStatusEntry{}
	ircStopMu   sync.Mutex
	ircCancel   chan struct{}
	ircWG       sync.WaitGroup
)

func setIRCStatus(id, host string, enabled, ok bool, detail string) {
	ircStatusMu.Lock()
	defer ircStatusMu.Unlock()
	prev := ircStatus[id]
	entry := ircStatusEntry{
		ID:                id,
		Host:              host,
		Enabled:           enabled,
		OK:                ok,
		Detail:            detail,
		LastAnnounceAt:    prev.LastAnnounceAt,
		LastAnnounceTitle: prev.LastAnnounceTitle,
		LastAnnounceChan:  prev.LastAnnounceChan,
		LastError:         prev.LastError,
	}
	if !ok && detail != "" && detail != "stopped" && detail != "disabled or incomplete" {
		entry.LastError = detail
	}
	ircStatus[id] = entry
}

func noteIRCAnnounce(id, channel, title string) {
	ircStatusMu.Lock()
	defer ircStatusMu.Unlock()
	entry := ircStatus[id]
	entry.ID = id
	entry.LastAnnounceAt = time.Now().UTC().Format(time.RFC3339)
	entry.LastAnnounceTitle = title
	entry.LastAnnounceChan = channel
	ircStatus[id] = entry
}

func getIRCStatus() []ircStatusEntry {
	ircStatusMu.Lock()
	defer ircStatusMu.Unlock()
	out := make([]ircStatusEntry, 0, len(ircStatus))
	for _, v := range ircStatus {
		out = append(out, v)
	}
	return out
}

func stopIRCLoops() {
	ircStopMu.Lock()
	defer ircStopMu.Unlock()
	if ircCancel != nil {
		close(ircCancel)
		ircCancel = nil
		ircWG.Wait()
	}
}

func restartIRCLoops() {
	stopIRCLoops()
	ircStopMu.Lock()
	ircCancel = make(chan struct{})
	stopCh := ircCancel
	ircStopMu.Unlock()

	st, err := loadSettings()
	if err != nil {
		return
	}
	for _, srv := range st.IRCServers {
		if !srv.Enabled || strings.TrimSpace(srv.Host) == "" || strings.TrimSpace(srv.Nick) == "" {
			setIRCStatus(srv.ID, srv.Host, srv.Enabled, false, "disabled or incomplete")
			continue
		}
		s := srv
		ircWG.Add(1)
		go func() {
			defer ircWG.Done()
			runIRCServer(s, stopCh)
		}()
	}
}

func runIRCServer(srv ircServer, stop <-chan struct{}) {
	backoff := time.Second
	for {
		select {
		case <-stop:
			setIRCStatus(srv.ID, srv.Host, true, false, "stopped")
			return
		default:
		}
		err := dialAndServeIRC(srv, stop)
		if err != nil {
			setIRCStatus(srv.ID, srv.Host, true, false, err.Error())
		}
		select {
		case <-stop:
			setIRCStatus(srv.ID, srv.Host, true, false, "stopped")
			return
		case <-time.After(backoff):
		}
		if backoff < 60*time.Second {
			backoff *= 2
		}
	}
}

func dialAndServeIRC(srv ircServer, stop <-chan struct{}) error {
	port := srv.Port
	if port <= 0 {
		if srv.TLS {
			port = 6697
		} else {
			port = 6667
		}
	}
	addr := fmt.Sprintf("%s:%d", srv.Host, port)
	var conn net.Conn
	var err error
	dialer := net.Dialer{Timeout: 20 * time.Second}
	if srv.TLS {
		conn, err = tls.DialWithDialer(&dialer, "tcp", addr, &tls.Config{ServerName: srv.Host, MinVersion: tls.VersionTLS12})
	} else {
		conn, err = dialer.Dial("tcp", addr)
	}
	if err != nil {
		return err
	}
	defer conn.Close()
	setIRCStatus(srv.ID, srv.Host, true, true, "connected")

	if srv.Password != "" {
		fmt.Fprintf(conn, "PASS %s\r\n", srv.Password)
	}
	fmt.Fprintf(conn, "NICK %s\r\n", srv.Nick)
	fmt.Fprintf(conn, "USER %s 0 * :Fleet AutoDL\r\n", srv.Nick)

	done := make(chan struct{})
	go func() {
		select {
		case <-stop:
			_ = conn.Close()
		case <-done:
		}
	}()
	defer close(done)

	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	joined := false
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "PING ") {
			fmt.Fprintf(conn, "PONG %s\r\n", strings.TrimPrefix(line, "PING "))
			continue
		}
		// Wait for welcome (001) or end of MOTD (376/422) before NickServ + JOIN.
		if !joined && (strings.Contains(line, " 001 ") || strings.Contains(line, " 376 ") || strings.Contains(line, " 422 ")) {
			if ns := strings.TrimSpace(srv.NickServPassword); ns != "" {
				fmt.Fprintf(conn, "PRIVMSG NickServ :IDENTIFY %s\r\n", ns)
			}
			for _, ch := range srv.Channels {
				fmt.Fprintf(conn, "JOIN %s\r\n", ch)
			}
			joined = true
			setIRCStatus(srv.ID, srv.Host, true, true, "joined")
		}
		pm, ok := parsePRIVMSG(line)
		if !ok {
			continue
		}
		handleIRCAnnounce(srv.ID, pm.Channel, pm.Text)
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return fmt.Errorf("connection closed")
}

func parsePRIVMSG(line string) (privmsg, bool) {
	// :nick!user@host PRIVMSG #chan :message
	if !strings.Contains(line, " PRIVMSG ") {
		return privmsg{}, false
	}
	priv := strings.Index(line, " PRIVMSG ")
	rest := line[priv+len(" PRIVMSG "):]
	colon := strings.Index(rest, " :")
	if colon < 0 {
		return privmsg{}, false
	}
	target := strings.TrimSpace(rest[:colon])
	text := rest[colon+2:]
	return privmsg{Channel: target, Text: text}, true
}

func handleIRCAnnounce(serverID, channel, msg string) {
	st, err := loadSettings()
	if err != nil || !st.Enabled || st.Paused {
		return
	}
	a, ok := parseAnnounce(msg)
	if !ok {
		return
	}
	a.ServerID = serverID
	a.Channel = channel
	noteIRCAnnounce(serverID, channel, a.Title)
	noteAnnounceSeen(serverID)
	f, ok := firstMatchingFilter(st.Filters, a)
	if !ok {
		noteAnnounceReject(serverID)
		return
	}
	noteAnnounceGrab(serverID)
	_ = processGrab(st, f, a, "irc")
}
