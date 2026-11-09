package protocol

import (
	"bufio"
	"fmt"
	"net/http"
)

// sprintf 是 fmt.Sprintf 的薄封装，便于 canonical.go 保持无 import。
func sprintf(format string, args ...any) string { return fmt.Sprintf(format, args...) }

// SSEWriter 负责按 Server-Sent Events 协议向客户端写帧。
type SSEWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
	started bool
}

// NewSSEWriter 构造 SSE 写入器并写入响应头。
func NewSSEWriter(w http.ResponseWriter) *SSEWriter {
	sw := &SSEWriter{w: w}
	if f, ok := w.(http.Flusher); ok {
		sw.flusher = f
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream; charset=utf-8")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	return sw
}

// Event 写出一个 SSE 事件帧（带 event 名与 data 负载）。
func (s *SSEWriter) Event(name string, payload any) error {
	if !s.started {
		s.started = true
		s.w.WriteHeader(http.StatusOK)
	}
	data, err := marshalJSON(payload)
	if err != nil {
		return err
	}
	if name != "" {
		if _, err := fmt.Fprintf(s.w, "event: %s\n", name); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(s.w, "data: %s\n\n", data); err != nil {
		return err
	}
	s.flush()
	return nil
}

// Data 写出一个匿名 SSE 帧（仅 data 行）。
func (s *SSEWriter) Data(payload any) error { return s.Event("", payload) }

// Done 写出结束帧并结束响应。
func (s *SSEWriter) Done() error {
	if s.flusher != nil {
		s.flusher.Flush()
	}
	return nil
}

// flush 推送缓冲。
func (s *SSEWriter) flush() {
	if s.flusher != nil {
		s.flusher.Flush()
	}
}

// ScanLines 是 bufio.Scanner 的行扫描封装，供各协议解析 SSE 输入复用。
func ScanLines(data []byte, fn func(line string) error) error {
	sc := bufio.NewScanner(newBytesReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 8<<20)
	for sc.Scan() {
		if err := fn(sc.Text()); err != nil {
			return err
		}
	}
	return sc.Err()
}
