package network

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

func (s *Server) handleDiagnostics(conn net.Conn) {
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	_ = conn.SetReadDeadline(time.Time{})
	if err != nil {
		_, _ = fmt.Fprintln(conn, "ok")
		return
	}
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "GET ") {
		_, _ = fmt.Fprintln(conn, "ok")
		return
	}
	path := "/"
	parts := strings.Fields(line)
	if len(parts) >= 2 {
		path = parts[1]
	}
	host := ""
	for {
		header, err := reader.ReadString('\n')
		if err != nil || strings.TrimSpace(header) == "" {
			break
		}
		if name, value, ok := strings.Cut(strings.TrimSpace(header), ":"); ok && strings.EqualFold(name, "Host") {
			host = strings.TrimSpace(value)
		}
	}
	body, contentType := s.diagnosticsHTTPBody(path, diagnosticsHost(host))
	writeHTTP(conn, "200 OK", contentType, body)
}

func (s *Server) diagnosticsHTTPBody(path string, host string) (string, string) {
	if host == "" {
		host = "127.0.0.1"
	}
	switch strings.ToLower(path) {
	case "/snda_list.dat", "/mir2/snda_list.dat", "/loader.ini":
		return strings.Join([]string{
			"[hot]",
			"count=1",
			"groupname0=" + s.serverName,
			"desc0=OpenMir2 local debug server",
			"",
			"[Area]",
			"MaxCount = 1",
			"Area0=OpenMir2|Local|" + s.serverName + "|" + s.serverName,
			"",
		}, "\r\n"), "text/plain; charset=gbk"
	case "/oemserverlist.xml", "/serverlist/st185/oemserverlist.xml":
		return strings.Join([]string{
			`<?xml version="1.0" encoding="GBK"?>`,
			`<root>`,
			`  <serverlistv2>`,
			`    <server id="1" realareaid="1" ip="` + host + `" issetip="1" flag="new" groupid="1" clientname="` + s.serverName + `" servername="` + s.serverName + `" port="7000"></server>`,
			`  </serverlistv2>`,
			`  <serverlistfortest></serverlistfortest>`,
			`</root>`,
			"",
		}, "\r\n"), "text/xml; charset=gbk"
	default:
		return "ok\r\n", "text/plain; charset=utf-8"
	}
}

func diagnosticsHost(host string) string {
	if host == "" {
		return ""
	}
	if strings.HasPrefix(host, "[") {
		if end := strings.Index(host, "]"); end >= 0 {
			return host[1:end]
		}
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	if h, _, ok := strings.Cut(host, ":"); ok {
		return h
	}
	return host
}

func writeHTTP(w io.Writer, status string, contentType string, body string) {
	_, _ = fmt.Fprintf(w, "HTTP/1.1 %s\r\n", status)
	_, _ = fmt.Fprintf(w, "Content-Type: %s\r\n", contentType)
	_, _ = fmt.Fprintf(w, "Content-Length: %d\r\n", len([]byte(body)))
	_, _ = fmt.Fprint(w, "Connection: close\r\n")
	_, _ = fmt.Fprint(w, "\r\n")
	_, _ = fmt.Fprint(w, body)
}
