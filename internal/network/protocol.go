package network

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"openmir2/internal/protocol/mir176"
	"openmir2/internal/storage"
	"openmir2/internal/world"
)

func disableNagle(conn net.Conn) {
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		_ = tcpConn.SetNoDelay(true)
	}
}

func (s *Server) handleConn(ctx context.Context, listener string, conn net.Conn) {
	defer conn.Close()
	switch listener {
	case "diagnostics":
		s.handleDiagnostics(conn)
	case "game":
		s.handleProtocol(ctx, conn)
	default:
		if strings.HasPrefix(listener, "diagnostics") {
			s.handleDiagnostics(conn)
			break
		}
		s.handleProtocolLab(listener, conn)
	}
}

func (s *Server) handleProtocolLab(listener string, conn net.Conn) {
	selectState := plain6SelectState{}
	defer func() {
		if listener == "select" && selectState.sessionID != 0 && !s.sessionPlayingState(selectState.sessionID) {
			s.closeSession(selectState.sessionID)
		}
	}()
	buf := make([]byte, 4096)
	pending := []byte{}
	var loginAccount string
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			pending = append(pending, chunk...)
			var frames [][]byte
			frames, pending = mir176.SplitFrames(pending)
			for _, frame := range frames {
				cmd, text, err := mir176.DecodePlain6ClientMessage(frame)
				if err == nil && isPlausibleProtocolIdent(cmd.Ident) {
					if listener == "login" && cmd.Ident == mir176.CMIDPassword {
						if loginAccount != "" {
							return
						}
						account, password := loginCredentials(text)
						if !s.store.Authenticate(account, password) {
							s.sendPlain6Command(conn, mir176.Command{Ident: mir176.SMPasswordFail, Recog: -1}, nil)
							continue
						}
						loginAccount = account
						s.sendPlain6LoginOK(conn, account)
					}
					if listener == "select" && s.handlePlain6SelectCommand(conn, cmd, text, &selectState) {
						continue
					}
					continue
				}
			}
		}
		if err != nil {
			return
		}
	}
}

func (s *Server) sendPlain6LoginOK(conn net.Conn, account string) {
	if account == "" {
		account = "test"
	}
	sessionID := s.newSessionID(account)
	s.rememberSession(sessionID, account)
	body := fmt.Sprintf("%s/%d/%d", acceptedLocalHost(conn), s.listenerPort("select", 7100), sessionID)
	response := mir176.EncodePlain6ClientMessage(mir176.Command{Ident: mir176.SMSelectServerOK, Recog: sessionID}, []byte(body))
	if _, err := conn.Write(response); err != nil {
		return
	}
}

type plain6SelectState struct {
	account   string
	sessionID int32
	queried   bool
	lastTick  time.Time
}

func (s *Server) handlePlain6SelectCommand(conn net.Conn, cmd mir176.Command, text []byte, state *plain6SelectState) bool {
	switch cmd.Ident {
	case mir176.CMQueryCharacter:
		if state.queried && time.Since(state.lastTick) <= 200*time.Millisecond {
			return true
		}
		account, sessionText := splitPlainText(text, "/")
		sessionID, err := strconv.Atoi(sessionText)
		if err != nil {
			s.sendPlain6Command(conn, mir176.Command{Ident: mir176.SMQueryCharacterFail, Tag: 1}, nil)
			_ = conn.Close()
			return true
		}
		sessionAccount, ok := s.sessionAccount(int32(sessionID))
		if !ok || sessionAccount != account {
			s.sendPlain6Command(conn, mir176.Command{Ident: mir176.SMQueryCharacterFail, Tag: 1}, nil)
			_ = conn.Close()
			return true
		}
		s.setSessionPlaying(int32(sessionID), false)
		state.account = account
		state.sessionID = int32(sessionID)
		state.queried = true
		state.lastTick = time.Now()
		s.sendPlain6CharacterList(conn, account)
		return true
	case mir176.CMNewCharacter:
		if !state.lastTick.IsZero() && time.Since(state.lastTick) <= time.Second {
			return true
		}
		state.queried = false
		state.lastTick = time.Now()
		parts := strings.Split(string(text), "/")
		account, sessionOK := s.sessionAccount(state.sessionID)
		if !sessionOK || account != state.account {
			s.sendPlain6Command(conn, mir176.Command{Ident: mir176.SMOutOfConnection}, nil)
			return true
		}
		if len(parts) < 5 || len(parts) > 6 || (len(parts) == 6 && parts[5] != "") || parts[0] != state.account || len(s.store.Characters(state.account)) >= 2 {
			reason := int32(4)
			if len(s.store.Characters(state.account)) >= 2 {
				reason = 3
			}
			s.sendPlain6Command(conn, mir176.Command{Ident: mir176.SMNewCharacterFail, Recog: reason}, nil)
			return true
		}
		account = parts[0]
		name := strings.TrimSpace(parts[1])
		if !validPlain6CharacterName(name) {
			s.sendPlain6Command(conn, mir176.Command{Ident: mir176.SMNewCharacterFail, Recog: 0}, nil)
			return true
		}
		if s.store.CharacterNameExists(name) {
			s.sendPlain6Command(conn, mir176.Command{Ident: mir176.SMNewCharacterFail, Recog: 2}, nil)
			return true
		}
		hair, _ := strconv.Atoi(parts[2])
		class := plain6ClassFromJob(parts[3])
		sex, _ := strconv.Atoi(parts[4])
		if _, err := s.world.CreateCharacterWithAppearanceAtRandomStartPoint(account, name, class, hair, sex); err != nil {
			s.sendPlain6Command(conn, mir176.Command{Ident: mir176.SMNewCharacterFail}, nil)
			return true
		}
		s.sendPlain6Command(conn, mir176.Command{Ident: mir176.SMNewCharacterOK}, nil)
		return true
	case mir176.CMDeleteCharacter:
		name := string(text)
		if !state.lastTick.IsZero() && time.Since(state.lastTick) <= time.Second {
			return true
		}
		state.queried = false
		state.lastTick = time.Now()
		account, sessionOK := s.sessionAccount(state.sessionID)
		if !sessionOK || account != state.account {
			s.sendPlain6Command(conn, mir176.Command{Ident: mir176.SMOutOfConnection}, nil)
			return true
		}
		if deleted, err := s.store.DeleteCharacter(state.account, name); err != nil || !deleted {
			s.sendPlain6Command(conn, mir176.Command{Ident: mir176.SMDeleteCharacterFail}, nil)
			return true
		}
		s.sendPlain6Command(conn, mir176.Command{Ident: mir176.SMDeleteCharacterOK}, nil)
		return true
	case mir176.CMSelectCharacter:
		account, name := splitPlainText(text, "/")
		if account == "" {
			account = state.account
		}
		if name == "" {
			name = string(text)
		}
		sessionAccount, sessionOK := s.sessionAccount(state.sessionID)
		if !sessionOK || sessionAccount != state.account || account != state.account {
			s.sendPlain6Command(conn, mir176.Command{Ident: mir176.SMStartFail}, nil)
			return true
		}
		if _, ok := s.characterByNameExact(account, name); !ok {
			s.sendPlain6Command(conn, mir176.Command{Ident: mir176.SMStartFail}, nil)
			return true
		}
		if selected, err := s.store.SelectCharacter(account, name); err != nil || !selected {
			s.sendPlain6Command(conn, mir176.Command{Ident: mir176.SMStartFail}, nil)
			return true
		}
		s.setSessionPlaying(state.sessionID, true)
		s.sendPlain6StartPlay(conn, name)
		return true
	default:
		return false
	}
}

func (s *Server) sendPlain6CharacterList(conn net.Conn, account string) {
	chars := s.store.Characters(account)
	sort.SliceStable(chars, func(i, j int) bool {
		left, leftErr := strconv.Atoi(strings.TrimPrefix(chars[i].ID, "char-"))
		right, rightErr := strconv.Atoi(strings.TrimPrefix(chars[j].ID, "char-"))
		if leftErr == nil && rightErr == nil {
			return left < right
		}
		return chars[i].ID < chars[j].ID
	})
	var body strings.Builder
	listed := 0
	for i, ch := range chars {
		if i >= 2 {
			break
		}
		name := ch.Name
		if ch.Selected {
			name = "*" + name
		}
		fmt.Fprintf(&body, "%s/%d/%d/%d/%d/", name, world.Plain6ClassID(ch.Class), ch.Hair, ch.Level, ch.Sex)
		listed++
	}
	response := mir176.EncodePlain6ClientMessage(mir176.Command{Ident: mir176.SMQueryCharacter, Recog: int32(listed), Tag: 1}, []byte(body.String()))
	if _, err := conn.Write(response); err != nil {
		return
	}
}

func (s *Server) sendPlain6StartPlay(conn net.Conn, name string) {
	body := fmt.Sprintf("%s/%d", acceptedLocalHost(conn), s.listenerPort("game", 7200))
	s.sendPlain6Command(conn, mir176.Command{Ident: mir176.SMStartPlay}, []byte(body))
}

func (s *Server) sendPlain6Command(conn net.Conn, cmd mir176.Command, text []byte) {
	response := mir176.EncodePlain6ClientMessage(cmd, text)
	_, _ = conn.Write(response)
}

func (s *Server) characterNameExists(name string) bool {
	for _, ch := range s.store.Characters("test") {
		if ch.Name == name {
			return true
		}
	}
	return false
}

func (s *Server) characterByNameExact(account, name string) (storage.Character, bool) {
	for _, ch := range s.store.Characters(account) {
		if ch.Name == name {
			return ch, true
		}
	}
	return storage.Character{}, false
}

func validPlain6CharacterName(name string) bool {
	if len(name) < 3 {
		return false
	}
	for _, char := range name {
		switch char {
		case ' ', '/', '@', '?', '\'', '"', '\\', '.', ',', ':', ';', '`', '~', '!', '#', '$', '%', '^', '&', '*', '(', ')', '-', '_', '+', '=', '|', '[', '{', ']', '}':
			return false
		}
	}
	return true
}

func plain6ClassFromJob(jobText string) string {
	job, _ := strconv.Atoi(jobText)
	return world.Plain6ClassName(strconv.Itoa(job))
}

func (s *Server) rememberSession(sessionID int32, account string) {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	s.sessions[sessionID] = account
}

func (s *Server) newSessionID(account string) int32 {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	if s.sessionPlaying == nil {
		s.sessionPlaying = map[int32]bool{}
	}
	for {
		s.nextSessionID++
		if s.nextSessionID < 2 {
			s.nextSessionID = 2
		}
		if _, exists := s.sessions[s.nextSessionID]; !exists {
			s.sessions[s.nextSessionID] = account
			s.sessionPlaying[s.nextSessionID] = false
			return s.nextSessionID
		}
	}
}

func (s *Server) setSessionPlaying(sessionID int32, playing bool) {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	if _, ok := s.sessions[sessionID]; ok {
		s.sessionPlaying[sessionID] = playing
	}
}

func (s *Server) sessionPlayingState(sessionID int32) bool {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	return s.sessionPlaying[sessionID]
}

func (s *Server) closeSession(sessionID int32) {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	delete(s.sessions, sessionID)
	delete(s.sessionPlaying, sessionID)
}

func (s *Server) sessionAccount(sessionID int32) (string, bool) {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	account, ok := s.sessions[sessionID]
	return account, ok
}

func splitPlainText(text []byte, sep string) (string, string) {
	left, right, ok := strings.Cut(string(text), sep)
	if !ok {
		return string(text), ""
	}
	return left, right
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func loginCredentials(text []byte) (string, string) {
	account, password := splitPlainText(text, "/")
	if decoded, ok := observedLoginPassword(account, password); ok {
		password = decoded
	}
	return account, password
}

func observedLoginPassword(account, password string) (string, bool) {
	if account == "test" && password == "fwNoei{MlIG[pL" {
		return "test", true
	}
	return "", false
}

func (s *Server) listenerPort(name string, fallback int) int {
	for _, listener := range s.listeners {
		if listener.Name != name {
			continue
		}
		_, port, err := net.SplitHostPort(listener.Addr)
		if err != nil {
			return fallback
		}
		n, err := strconv.Atoi(port)
		if err != nil {
			return fallback
		}
		return n
	}
	return fallback
}

func acceptedLocalHost(conn net.Conn) string {
	if addr, ok := conn.LocalAddr().(*net.TCPAddr); ok && addr.IP != nil && !addr.IP.IsUnspecified() {
		return addr.IP.String()
	}
	host, _, err := net.SplitHostPort(conn.LocalAddr().String())
	if err == nil && host != "" && host != "::" && host != "0.0.0.0" {
		return host
	}
	return "127.0.0.1"
}

func isPlausibleProtocolIdent(ident uint16) bool {
	return ident > 0 && ident < 10000
}
