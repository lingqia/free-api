package gateway

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// consoleCookieName 是控制台登录会话 Cookie。
	consoleCookieName = "freeapi_console"
	// sessionTTL 是控制台登录有效期。
	sessionTTL = 7 * 24 * time.Hour
	// passwordIterations 是密码哈希迭代次数。
	passwordIterations = 120000
)

// Settings 保存控制台登录密码与网关 API Key。
// 密码只保存加盐哈希；会话只存在内存中，重启后需要重新登录。
type Settings struct {
	mu       sync.RWMutex
	path     string
	salt     string
	hash     string
	apiKeys  []string
	disabled map[string]bool // keyID -> 已停用
	sessions map[string]time.Time
}

// KeyInfo 是控制台展示的网关 API Key 信息（不含明文）。
type KeyInfo struct {
	ID      string `json:"id"`
	Masked  string `json:"masked"`
	Enabled bool   `json:"enabled"`
}

// settingsDisk 是设置文件的磁盘格式。
// APIKeys 使用指针以区分「配置文件没有该字段」和「明确设置为空」。
// DisabledKeyIDs 只保存 Key 的哈希短标识，不额外复制明文。
type settingsDisk struct {
	PasswordSalt   string    `json:"password_salt,omitempty"`
	PasswordHash   string    `json:"password_hash,omitempty"`
	APIKeys        *[]string `json:"api_keys,omitempty"`
	DisabledKeyIDs []string  `json:"disabled_key_ids,omitempty"`
}

// LoadSettings 读取设置文件；文件不存在时使用配置里的 API Key 作为初始值。
func LoadSettings(path string, fallbackKeys []string) (*Settings, error) {
	s := &Settings{
		path:     path,
		apiKeys:  normalizeAPIKeys(fallbackKeys),
		disabled: map[string]bool{},
		sessions: map[string]time.Time{},
	}
	if path == "" {
		return s, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return s, nil
		}
		return nil, fmt.Errorf("读取设置文件失败: %w", err)
	}
	if len(raw) == 0 {
		return s, nil
	}
	var d settingsDisk
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("解析设置文件失败: %w", err)
	}
	s.salt = d.PasswordSalt
	s.hash = d.PasswordHash
	if d.APIKeys != nil {
		s.apiKeys = normalizeAPIKeys(*d.APIKeys)
	}
	for _, id := range d.DisabledKeyIDs {
		s.disabled[id] = true
	}
	return s, nil
}

// newMemorySettings 用于没有显式传入设置对象的调用方（保留原 API Key 行为）。
func newMemorySettings(keys []string) *Settings {
	return &Settings{
		apiKeys:  normalizeAPIKeys(keys),
		disabled: map[string]bool{},
		sessions: map[string]time.Time{},
	}
}

// PasswordSet 返回是否启用了控制台登录密码。
func (s *Settings) PasswordSet() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.hash != ""
}

// APIKeys 返回当前网关 API Key 的副本。
func (s *Settings) APIKeys() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]string{}, s.apiKeys...)
}

// SetAPIKeys 覆盖网关 API Key 并持久化。
func (s *Settings) SetAPIKeys(keys []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.apiKeys = normalizeAPIKeys(keys)
	s.pruneDisabledLocked()
	return s.saveLocked()
}

// AddAPIKeys 追加 API Key（去重），保持既有顺序。
func (s *Settings) AddAPIKeys(keys []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	merged := append(append([]string{}, s.apiKeys...), keys...)
	s.apiKeys = normalizeAPIKeys(merged)
	return s.saveLocked()
}

// KeyInfos 返回所有网关 API Key 的展示信息（已遮蔽明文）。
func (s *Settings) KeyInfos() []KeyInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]KeyInfo, 0, len(s.apiKeys))
	for _, k := range s.apiKeys {
		id := keyID(k)
		out = append(out, KeyInfo{ID: id, Masked: maskKey(k), Enabled: !s.disabled[id]})
	}
	return out
}

// ValidAPIKey 判断 API Key 是否有效且未被停用。
func (s *Settings) ValidAPIKey(token string) bool {
	if token == "" {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.disabled[keyID(token)] {
		return false
	}
	for _, k := range s.apiKeys {
		if k == token {
			return true
		}
	}
	return false
}

// ToggleAPIKey 启用或停用某个 Key。
func (s *Settings) ToggleAPIKey(id string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.hasKeyLocked(id) {
		return errors.New("Key 不存在")
	}
	if s.disabled == nil {
		s.disabled = map[string]bool{}
	}
	if enabled {
		delete(s.disabled, id)
	} else {
		s.disabled[id] = true
	}
	return s.saveLocked()
}

// DeleteAPIKey 删除某个 Key。
func (s *Settings) DeleteAPIKey(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.apiKeys))
	found := false
	for _, k := range s.apiKeys {
		if keyID(k) == id {
			found = true
			continue
		}
		out = append(out, k)
	}
	if !found {
		return errors.New("Key 不存在")
	}
	s.apiKeys = out
	s.pruneDisabledLocked()
	return s.saveLocked()
}

// keyID 返回 Key 的稳定短标识，用于控制台停用/删除，不暴露明文。
func keyID(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])[:12]
}

// maskKey 遮蔽 Key 的中间部分，只保留首尾便于辨认。
func maskKey(key string) string {
	if len(key) <= 2 {
		return key
	}
	if len(key) <= 8 {
		return key[:1] + strings.Repeat("*", len(key)-1)
	}
	return key[:4] + strings.Repeat("*", 4) + key[len(key)-4:]
}

// hasKeyLocked 判断指定 ID 的 Key 是否存在，调用方需持有写锁。
func (s *Settings) hasKeyLocked(id string) bool {
	for _, k := range s.apiKeys {
		if keyID(k) == id {
			return true
		}
	}
	return false
}

// pruneDisabledLocked 清理已经不存在的 Key 的停用标记。
func (s *Settings) pruneDisabledLocked() {
	if len(s.disabled) == 0 {
		return
	}
	valid := map[string]bool{}
	for _, k := range s.apiKeys {
		valid[keyID(k)] = true
	}
	for id := range s.disabled {
		if !valid[id] {
			delete(s.disabled, id)
		}
	}
}

// SetPassword 设置登录密码；传空字符串表示关闭密码保护。
// 修改密码后所有旧会话立即失效。
func (s *Settings) SetPassword(password string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if password == "" {
		s.salt = ""
		s.hash = ""
	} else {
		salt := make([]byte, 16)
		if _, err := rand.Read(salt); err != nil {
			return fmt.Errorf("生成密码盐失败: %w", err)
		}
		s.salt = base64.StdEncoding.EncodeToString(salt)
		s.hash = hashPassword(password, s.salt)
	}
	s.sessions = map[string]time.Time{}
	return s.saveLocked()
}

// VerifyPassword 校验登录密码。
func (s *Settings) VerifyPassword(password string) bool {
	s.mu.RLock()
	salt, hash := s.salt, s.hash
	s.mu.RUnlock()
	if salt == "" || hash == "" {
		return false
	}
	got := hashPassword(password, salt)
	return subtle.ConstantTimeCompare([]byte(got), []byte(hash)) == 1
}

// CreateSession 创建新的登录会话并返回令牌。
func (s *Settings) CreateSession() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成会话令牌失败: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(buf)
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessions == nil {
		s.sessions = map[string]time.Time{}
	}
	for k, exp := range s.sessions {
		if now.After(exp) {
			delete(s.sessions, k)
		}
	}
	s.sessions[token] = now.Add(sessionTTL)
	return token, nil
}

// ValidSession 判断请求携带的控制台会话是否有效。
func (s *Settings) ValidSession(r *http.Request) bool {
	c, err := r.Cookie(consoleCookieName)
	if err != nil || c.Value == "" {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	exp, ok := s.sessions[c.Value]
	return ok && time.Now().Before(exp)
}

// DropSession 注销单个会话。
func (s *Settings) DropSession(token string) {
	if token == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, token)
}

// saveLocked 原子写回设置文件。
func (s *Settings) saveLocked() error {
	if s.path == "" {
		return nil
	}
	if dir := filepath.Dir(s.path); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("创建设置目录失败: %w", err)
		}
	}
	keys := append([]string{}, s.apiKeys...)
	disabledIDs := make([]string, 0, len(s.disabled))
	for id := range s.disabled {
		disabledIDs = append(disabledIDs, id)
	}
	sort.Strings(disabledIDs)
	d := settingsDisk{
		PasswordSalt:   s.salt,
		PasswordHash:   s.hash,
		APIKeys:        &keys,
		DisabledKeyIDs: disabledIDs,
	}
	raw, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return fmt.Errorf("写入设置临时文件失败: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("提交设置文件失败: %w", err)
	}
	return nil
}

// hashPassword 使用加盐 SHA-256 迭代生成密码哈希。
func hashPassword(password, salt string) string {
	sum := sha256.Sum256([]byte(salt + "\x00" + password))
	buf := sum[:]
	for i := 0; i < passwordIterations; i++ {
		h := sha256.New()
		h.Write(buf)
		h.Write([]byte(salt))
		buf = h.Sum(nil)
	}
	return base64.StdEncoding.EncodeToString(buf)
}

// normalizeAPIKeys 去掉空白与重复项，保持原有顺序。
func normalizeAPIKeys(keys []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, key)
	}
	return out
}

// sessionTokenFromRequest 读取控制台会话 Cookie。
func sessionTokenFromRequest(r *http.Request) string {
	c, err := r.Cookie(consoleCookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

// setSessionCookie 写入控制台会话 Cookie。
func setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     consoleCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(sessionTTL.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// clearSessionCookie 清除控制台会话 Cookie。
func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     consoleCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}
