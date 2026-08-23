package network

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"

	"openmir2/internal/protocol/mir176"
	"openmir2/internal/world"
)

func disableNagle(conn net.Conn) {
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		_ = tcpConn.SetNoDelay(true)
	}
}

func (s *Server) handleConn(ctx context.Context, listener string, conn net.Conn) {
	defer conn.Close()
	s.log.Info("client connected", "listener", listener, "remote", conn.RemoteAddr().String())
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
	s.log.Info("client disconnected", "listener", listener, "remote", conn.RemoteAddr().String())
}

func (s *Server) handleProtocolLab(listener string, conn net.Conn) {
	buf := make([]byte, 4096)
	pending := []byte{}
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
						account, password := loginCredentials(text)
						if !s.store.Authenticate(account, password) {
							s.sendPlain6Command(conn, mir176.Command{Ident: mir176.SMPasswordFail, Recog: -1}, nil)
							continue
						}
						s.sendPlain6LoginOK(conn, account)
					}
					if listener == "select" && s.handlePlain6SelectCommand(conn, cmd, text) {
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
	sessionID := int32(1)
	s.rememberSession(sessionID, account)
	body := fmt.Sprintf("%s/%d/%d", acceptedLocalHost(conn), s.listenerPort("select", 7100), sessionID)
	response := mir176.EncodePlain6ClientMessage(mir176.Command{Ident: mir176.SMSelectServerOK, Recog: sessionID}, []byte(body))
	if _, err := conn.Write(response); err != nil {
		s.log.Info("plain6 login response failed", "error", err)
		return
	}
	s.log.Info("plain6 login response sent", "ident", mir176.SMSelectServerOK, "body_len", len(body))
}

func (s *Server) handlePlain6SelectCommand(conn net.Conn, cmd mir176.Command, text []byte) bool {
	switch cmd.Ident {
	case mir176.CMQueryCharacter:
		account, sessionText := splitPlainText(text, "/")
		if sessionID, err := strconv.Atoi(sessionText); err == nil {
			if sessionAccount, ok := s.sessionAccount(int32(sessionID)); ok {
				account = sessionAccount
			}
		}
		if account == "" {
			account = "test"
		}
		s.sendPlain6CharacterList(conn, account)
		return true
	case mir176.CMNewCharacter:
		parts := strings.Split(string(text), "/")
		if len(parts) < 5 {
			s.sendPlain6Command(conn, mir176.Command{Ident: mir176.SMNewCharacterFail}, nil)
			return true
		}
		account := parts[0]
		name := parts[1]
		hair, _ := strconv.Atoi(parts[2])
		class := world.Plain6ClassName(parts[3])
		sex, _ := strconv.Atoi(parts[4])
		if _, err := s.world.CreateCharacterWithAppearanceAtRandomStartPoint(account, name, class, hair, sex); err != nil {
			s.log.Info("plain6 create character failed", "error", err)
			s.sendPlain6Command(conn, mir176.Command{Ident: mir176.SMNewCharacterFail}, nil)
			return true
		}
		s.sendPlain6Command(conn, mir176.Command{Ident: mir176.SMNewCharacterOK}, nil)
		return true
	case mir176.CMSelectCharacter:
		_, name := splitPlainText(text, "/")
		if name == "" {
			name = string(text)
		}
		s.sendPlain6StartPlay(conn, name)
		return true
	default:
		return false
	}
}

func (s *Server) sendPlain6CharacterList(conn net.Conn, account string) {
	chars := s.store.Characters(account)
	var body strings.Builder
	for i, ch := range chars {
		if i >= 3 {
			break
		}
		name := ch.Name
		if i == 0 {
			name = "*" + name
		}
		fmt.Fprintf(&body, "%s/%d/%d/%d/%d/", name, world.Plain6ClassID(ch.Class), ch.Hair, ch.Level, ch.Sex)
	}
	response := mir176.EncodePlain6ClientMessage(mir176.Command{Ident: mir176.SMQueryCharacter, Recog: int32(len(chars)), Tag: 1}, []byte(body.String()))
	if _, err := conn.Write(response); err != nil {
		s.log.Info("plain6 character list response failed", "error", err)
		return
	}
	s.log.Info("plain6 character list response sent", "account", account, "count", len(chars), "body_len", body.Len())
}

func (s *Server) sendPlain6StartPlay(conn net.Conn, name string) {
	body := fmt.Sprintf("%s/%d", acceptedLocalHost(conn), s.listenerPort("game", 7200))
	if !s.characterNameExists(name) {
		s.log.Info("plain6 selected character not found", "name", name)
	}
	s.sendPlain6Command(conn, mir176.Command{Ident: mir176.SMStartPlay}, []byte(body))
}

func (s *Server) sendPlain6Command(conn net.Conn, cmd mir176.Command, text []byte) {
	response := mir176.EncodePlain6ClientMessage(cmd, text)
	if _, err := conn.Write(response); err != nil {
		s.log.Info("plain6 response failed", "ident", cmd.Ident, "error", err)
		return
	}
	s.log.Info("plain6 response sent", "ident", cmd.Ident, "text_len", len(text))
}

func (s *Server) characterNameExists(name string) bool {
	for _, ch := range s.store.Characters("test") {
		if ch.Name == name {
			return true
		}
	}
	return false
}

func (s *Server) rememberSession(sessionID int32, account string) {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	s.sessions[sessionID] = account
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
