// Package gateway 的本地轮换代理。
//
// ProxyHub 在 127.0.0.1 上监听一个带随机 Basic 认证的 HTTP 代理端口，
// 网关启动插件时把 HTTP_PROXY / HTTPS_PROXY / ALL_PROXY 指向它。
// 插件不需要自己实现代理选择，任何遵循标准代理环境变量的 HTTP 客户端
// 都会自动走这里；Hub 每次连接从全局代理池轮换出口。
package gateway

import (
	"bufio"
	"crypto/rand"
	"crypto/subtle"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"free-api/internal/credential"
)

// ProxyHub 是网关内置的轮换代理入口。
type ProxyHub struct {
	creds *credential.Store
	ln    net.Listener
	srv   *http.Server
	user  string
	pass  string

	transportMu sync.Mutex
	transports  map[string]*http.Transport // 出口 URL -> 连接池

	// cursor 是 Hub 独立的轮换游标，与网关注入 credential.proxy 的游标互不影响。
	cursor uint64
}

// NewProxyHub 构造代理入口。creds 为空时所有请求直连。
func NewProxyHub(creds *credential.Store) *ProxyHub {
	return &ProxyHub{
		creds:      creds,
		transports: map[string]*http.Transport{},
	}
}

// Start 监听 127.0.0.1 的随机端口。
func (h *ProxyHub) Start() error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	h.ln = ln
	h.user = randomToken(12)
	h.pass = randomToken(24)
	h.srv = &http.Server{
		Handler:           http.HandlerFunc(h.handle),
		ReadHeaderTimeout: 15 * time.Second,
	}
	go func() {
		if err := h.srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			slog.Warn("本地代理服务退出", "err", err)
		}
	}()
	return nil
}

// Addr 返回监听地址（不含认证信息）。
func (h *ProxyHub) Addr() string {
	if h.ln == nil {
		return ""
	}
	return h.ln.Addr().String()
}

// Env 返回注入插件进程的代理环境变量。
func (h *ProxyHub) Env() []string {
	if h.ln == nil {
		return nil
	}
	proxyURL := fmt.Sprintf("http://%s:%s@%s", h.user, h.pass, h.ln.Addr().String())
	return []string{
		"HTTP_PROXY=" + proxyURL,
		"HTTPS_PROXY=" + proxyURL,
		"ALL_PROXY=" + proxyURL,
		"http_proxy=" + proxyURL,
		"https_proxy=" + proxyURL,
		"all_proxy=" + proxyURL,
		"NO_PROXY=127.0.0.1,localhost,::1",
		"no_proxy=127.0.0.1,localhost,::1",
	}
}

// Close 关闭代理服务。
func (h *ProxyHub) Close() {
	if h.srv != nil {
		_ = h.srv.Close()
	}
}

// pick 从全局代理池轮换取一个出口；池为空时返回 nil（直连）。
// 使用 Hub 自己的游标，避免与网关注入 credential.proxy 的轮换互相干扰。
func (h *ProxyHub) pick() (*url.URL, error) {
	if h.creds == nil {
		return nil, nil
	}
	lines := h.creds.Values(proxyPoolPlugin, "proxy")
	if len(lines) == 0 {
		return nil, nil
	}
	idx := atomic.AddUint64(&h.cursor, 1) - 1
	raw := lines[int(idx)%len(lines)]
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return nil, nil
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "socks5", "socks5h":
		return u, nil
	default:
		return nil, nil
	}
}

// transportFor 返回某个出口的连接池；相同出口复用 TCP 连接。
// 每个请求只在 handleForward 里选一次出口，避免 Transport 内部多次
// 调用 Proxy 函数导致轮换游标跳跃。
func (h *ProxyHub) transportFor(up *url.URL) *http.Transport {
	key := "direct"
	if up != nil {
		key = up.String()
	}
	h.transportMu.Lock()
	defer h.transportMu.Unlock()
	if tr, ok := h.transports[key]; ok {
		return tr
	}
	tr := &http.Transport{
		MaxIdleConns:          32,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		DisableCompression:    true,
	}
	if up != nil {
		tr.Proxy = http.ProxyURL(up)
	}
	h.transports[key] = tr
	return tr
}

// handle 处理代理请求：CONNECT 走隧道，普通 HTTP 走转发。
func (h *ProxyHub) handle(w http.ResponseWriter, r *http.Request) {
	if !h.authorized(r) {
		w.Header().Set("Proxy-Authenticate", `Basic realm="free-api"`)
		http.Error(w, "proxy authentication required", http.StatusProxyAuthRequired)
		return
	}
	if r.Method == http.MethodConnect {
		h.handleConnect(w, r)
		return
	}
	h.handleForward(w, r)
}

// authorized 校验代理入口的随机账号。
func (h *ProxyHub) authorized(r *http.Request) bool {
	auth := r.Header.Get("Proxy-Authorization")
	const prefix = "Basic "
	if !strings.HasPrefix(auth, prefix) {
		return false
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(strings.TrimPrefix(auth, prefix)))
	if err != nil {
		return false
	}
	user, pass, ok := strings.Cut(string(raw), ":")
	if !ok {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(user), []byte(h.user)) == 1 &&
		subtle.ConstantTimeCompare([]byte(pass), []byte(h.pass)) == 1
}

// handleForward 转发普通 HTTP 请求，出口按请求轮换。
func (h *ProxyHub) handleForward(w http.ResponseWriter, r *http.Request) {
	if !r.URL.IsAbs() {
		http.Error(w, "只接受代理形式的请求", http.StatusBadRequest)
		return
	}
	outReq := r.Clone(r.Context())
	outReq.RequestURI = ""
	removeHopHeaders(outReq.Header)

	up, _ := h.pick()
	resp, err := h.transportFor(up).RoundTrip(outReq)
	if err != nil {
		http.Error(w, "代理请求失败: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	removeHopHeaders(resp.Header)
	copyHeader(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// handleConnect 处理 HTTPS 隧道，出口按连接轮换。
func (h *ProxyHub) handleConnect(w http.ResponseWriter, r *http.Request) {
	target := r.Host
	if target == "" {
		http.Error(w, "缺少目标地址", http.StatusBadRequest)
		return
	}
	conn, err := h.dialTarget(target)
	if err != nil {
		http.Error(w, "代理连接失败: "+err.Error(), http.StatusBadGateway)
		return
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		conn.Close()
		http.Error(w, "不支持隧道", http.StatusInternalServerError)
		return
	}
	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		conn.Close()
		return
	}
	if _, err := clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		clientConn.Close()
		conn.Close()
		return
	}
	relay(clientConn, conn)
}

// dialTarget 通过当前轮换到的出口连接目标；没有代理时直连。
// 池里的出口失败时换下一个，最多尝试 3 次。
func (h *ProxyHub) dialTarget(target string) (net.Conn, error) {
	var lastErr error
	for i := 0; i < 3; i++ {
		up, err := h.pick()
		if err != nil || up == nil {
			return net.DialTimeout("tcp", target, 30*time.Second)
		}
		conn, err := dialViaProxy(up, target)
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("代理池没有可用出口")
	}
	return nil, lastErr
}

// dialViaProxy 通过上游代理建立到 target 的连接。
func dialViaProxy(up *url.URL, target string) (net.Conn, error) {
	scheme := strings.ToLower(up.Scheme)
	proxyAddr := up.Host
	if _, _, err := net.SplitHostPort(proxyAddr); err != nil {
		port := "80"
		if scheme == "https" {
			port = "443"
		}
		proxyAddr = net.JoinHostPort(up.Hostname(), port)
	}

	conn, err := net.DialTimeout("tcp", proxyAddr, 30*time.Second)
	if err != nil {
		return nil, err
	}
	if scheme == "https" {
		tlsConn := tls.Client(conn, &tls.Config{ServerName: up.Hostname()})
		if err := tlsConn.Handshake(); err != nil {
			conn.Close()
			return nil, err
		}
		conn = tlsConn
	}

	switch scheme {
	case "http", "https":
		req := "CONNECT " + target + " HTTP/1.1\r\nHost: " + target + "\r\n"
		if up.User != nil {
			pass, _ := up.User.Password()
			basic := base64.StdEncoding.EncodeToString([]byte(up.User.Username() + ":" + pass))
			req += "Proxy-Authorization: Basic " + basic + "\r\n"
		}
		req += "\r\n"
		if _, err := io.WriteString(conn, req); err != nil {
			conn.Close()
			return nil, err
		}
		br := bufio.NewReader(conn)
		resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodConnect})
		if err != nil {
			conn.Close()
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			conn.Close()
			return nil, fmt.Errorf("上游代理 CONNECT 返回 %s", resp.Status)
		}
		return &bufferedConn{Conn: conn, r: br}, nil

	case "socks5", "socks5h":
		host, _, err := net.SplitHostPort(target)
		if err != nil {
			conn.Close()
			return nil, err
		}
		// socks5（非 h）在本地解析域名；socks5h 交给代理解析。
		if scheme == "socks5" && net.ParseIP(host) == nil {
			ips, err := net.LookupHost(host)
			if err != nil || len(ips) == 0 {
				conn.Close()
				return nil, fmt.Errorf("解析目标域名失败: %v", err)
			}
			_, port, _ := net.SplitHostPort(target)
			target = net.JoinHostPort(ips[0], port)
		}
		if err := socks5Connect(conn, target, up.User); err != nil {
			conn.Close()
			return nil, err
		}
		return conn, nil
	}
	conn.Close()
	return nil, fmt.Errorf("不支持的代理协议 %q", up.Scheme)
}

// socks5Connect 执行 SOCKS5 握手与 CONNECT。
func socks5Connect(conn net.Conn, target string, auth *url.Userinfo) error {
	host, portStr, err := net.SplitHostPort(target)
	if err != nil {
		return err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 0 || port > 65535 {
		return fmt.Errorf("目标端口非法")
	}

	methods := []byte{0x00}
	if auth != nil {
		methods = []byte{0x00, 0x02}
	}
	if _, err := conn.Write(append([]byte{0x05, byte(len(methods))}, methods...)); err != nil {
		return err
	}
	sel := make([]byte, 2)
	if _, err := io.ReadFull(conn, sel); err != nil {
		return err
	}
	if sel[0] != 0x05 {
		return fmt.Errorf("SOCKS5 版本错误")
	}
	switch sel[1] {
	case 0x00:
	case 0x02:
		if auth == nil {
			return fmt.Errorf("SOCKS5 要求认证")
		}
		pass, _ := auth.Password()
		user := auth.Username()
		msg := []byte{0x01, byte(len(user))}
		msg = append(msg, user...)
		msg = append(msg, byte(len(pass)))
		msg = append(msg, pass...)
		if _, err := conn.Write(msg); err != nil {
			return err
		}
		ar := make([]byte, 2)
		if _, err := io.ReadFull(conn, ar); err != nil {
			return err
		}
		if ar[1] != 0x00 {
			return fmt.Errorf("SOCKS5 认证失败")
		}
	case 0xff:
		return fmt.Errorf("SOCKS5 没有可接受的认证方式")
	default:
		return fmt.Errorf("SOCKS5 不支持的认证方式: %d", sel[1])
	}

	req := []byte{0x05, 0x01, 0x00}
	if ip := net.ParseIP(host); ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			req = append(req, 0x01)
			req = append(req, ip4...)
		} else {
			req = append(req, 0x04)
			req = append(req, ip.To16()...)
		}
	} else {
		if len(host) > 255 {
			return fmt.Errorf("SOCKS5 域名过长")
		}
		req = append(req, 0x03, byte(len(host)))
		req = append(req, host...)
	}
	req = append(req, byte(port>>8), byte(port))
	if _, err := conn.Write(req); err != nil {
		return err
	}

	head := make([]byte, 4)
	if _, err := io.ReadFull(conn, head); err != nil {
		return err
	}
	if head[0] != 0x05 || head[1] != 0x00 {
		return fmt.Errorf("SOCKS5 CONNECT 失败（状态码 %d）", head[1])
	}
	skip := 0
	switch head[3] {
	case 0x01:
		skip = 4
	case 0x04:
		skip = 16
	case 0x03:
		l := make([]byte, 1)
		if _, err := io.ReadFull(conn, l); err != nil {
			return err
		}
		skip = int(l[0])
	default:
		return fmt.Errorf("SOCKS5 地址类型错误: %d", head[3])
	}
	tail := make([]byte, skip+2)
	_, err = io.ReadFull(conn, tail)
	return err
}

// bufferedConn 让 CONNECT 后残留的缓冲数据继续可读。
type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) { return c.r.Read(p) }

// relay 双向转发，任意一端关闭后关闭两端。
func relay(a, b net.Conn) {
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(a, b)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(b, a)
		done <- struct{}{}
	}()
	<-done
	a.Close()
	b.Close()
}

// removeHopHeaders 去掉逐跳头，避免代理链路污染。
func removeHopHeaders(h http.Header) {
	for _, k := range []string{
		"Proxy-Authorization", "Proxy-Connection", "Connection", "Keep-Alive",
		"Te", "Trailer", "Transfer-Encoding", "Upgrade",
	} {
		h.Del(k)
	}
}

// copyHeader 复制响应头。
func copyHeader(dst, src http.Header) {
	for k, values := range src {
		for _, v := range values {
			dst.Add(k, v)
		}
	}
}

// randomToken 生成十六进制随机串。
func randomToken(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(buf)
}
