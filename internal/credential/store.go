// Package credential 负责站点凭证的加密存储与按需注入。
//
// 设计约束：网关只保管使用者自行录入的凭证，不负责获取、注册或续期账号。
// 存储层是「只写不读」语义——列表接口只暴露字段名与是否已配置，
// 凭证明文仅在调用插件时按插件声明的字段名取出并注入。
package credential

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Store 是凭证存储。
type Store struct {
	mu   sync.RWMutex
	path string
	key  []byte
	data map[string]map[string]string // pluginName -> field -> ciphertext
	// cursor 记录多值字段的轮换位置，用于多 key / 多代理轮询。
	cursor map[string]uint64
}

// ErrNoKey 表示缺少加密密钥环境变量。
var ErrNoKey = errors.New("缺少凭证加密密钥：请设置 FREE_API_CRED_KEY")

// New 打开或创建凭证存储。加密密钥来自环境变量 FREE_API_CRED_KEY，
// 密钥本身不落盘；换密钥后旧密文将无法解密。
func New(path string) (*Store, error) {
	secret := os.Getenv("FREE_API_CRED_KEY")
	if secret == "" {
		return nil, ErrNoKey
	}
	sum := sha256.Sum256([]byte(secret))
	s := &Store{
		path:   path,
		key:    sum[:],
		data:   map[string]map[string]string{},
		cursor: map[string]uint64{},
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// load 读取磁盘上的密文数据。文件不存在视为空存储。
func (s *Store) load() error {
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("读取凭证文件失败: %w", err)
	}
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, &s.data)
}

// save 原子写回磁盘：先写临时文件再改名，避免半写状态。
func (s *Store) save() error {
	if dir := filepath.Dir(s.path); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("创建凭证目录失败: %w", err)
		}
	}
	raw, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return fmt.Errorf("写入凭证临时文件失败: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("提交凭证文件失败: %w", err)
	}
	return nil
}

// Set 写入（或覆盖）某插件的某个凭证字段。
func (s *Store) Set(plugin, field, value string) error {
	if plugin == "" || field == "" {
		return errors.New("plugin 与 field 均不能为空")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cipherText, err := s.encrypt(value)
	if err != nil {
		return err
	}
	if s.data[plugin] == nil {
		s.data[plugin] = map[string]string{}
	}
	s.data[plugin][field] = cipherText
	return s.save()
}

// Delete 删除某插件的某个凭证字段。
func (s *Store) Delete(plugin, field string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if fields, ok := s.data[plugin]; ok {
		delete(fields, field)
		if len(fields) == 0 {
			delete(s.data, plugin)
		}
	}
	return s.save()
}

// Fields 返回某插件已配置的字段名列表（不含任何明文）。
func (s *Store) Fields(plugin string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []string{}
	for f := range s.data[plugin] {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

// Has 判断某插件某字段是否已配置。
func (s *Store) Has(plugin, field string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	fields, ok := s.data[plugin]
	if !ok {
		return false
	}
	_, ok = fields[field]
	return ok
}

// Get 解密并返回某插件某字段的明文，仅供注入调用链使用。
func (s *Store) Get(plugin, field string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	fields, ok := s.data[plugin]
	if !ok {
		return "", false
	}
	cipherText, ok := fields[field]
	if !ok {
		return "", false
	}
	plain, err := s.decrypt(cipherText)
	if err != nil {
		return "", false
	}
	return plain, true
}

// Collect 按字段名清单取出某插件的凭证，返回字段名到明文的映射。
// 未配置的字段不会出现在结果中。
func (s *Store) Collect(plugin string, fields []string) map[string]string {
	out := map[string]string{}
	for _, f := range fields {
		if v, ok := s.Get(plugin, f); ok {
			out[f] = v
		}
	}
	return out
}

// CountLines 返回某字段中非空行的数量（多 key / 多代理按行分隔）。
func (s *Store) CountLines(plugin, field string) int {
	v, ok := s.Get(plugin, field)
	if !ok {
		return 0
	}
	return len(nonEmptyLines(v))
}

// Values 返回多值字段的全部明文，供网关内部轮换使用（不经过共享游标）。
func (s *Store) Values(plugin, field string) []string {
	v, ok := s.Get(plugin, field)
	if !ok {
		return nil
	}
	return nonEmptyLines(v)
}

// Pick 从多值字段中按轮换顺序取一个值，用于多 key / 多代理负载均衡。
func (s *Store) Pick(plugin, field string) (string, bool) {
	v, ok := s.Get(plugin, field)
	if !ok {
		return "", false
	}
	lines := nonEmptyLines(v)
	if len(lines) == 0 {
		return "", false
	}
	if len(lines) == 1 {
		return lines[0], true
	}
	key := plugin + "\x00" + field
	s.mu.Lock()
	if s.cursor == nil {
		s.cursor = map[string]uint64{}
	}
	idx := s.cursor[key]
	s.cursor[key] = idx + 1
	s.mu.Unlock()
	return lines[int(idx)%len(lines)], true
}

// CollectOne 与 Collect 类似，但每个多值字段只取一个轮换值。
func (s *Store) CollectOne(plugin string, fields []string) map[string]string {
	out := map[string]string{}
	for _, f := range fields {
		if v, ok := s.Pick(plugin, f); ok {
			out[f] = v
		}
	}
	return out
}

// nonEmptyLines 按行拆分并去掉空行。
func nonEmptyLines(v string) []string {
	out := []string{}
	for _, line := range strings.Split(v, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

// encrypt 使用 AES-256-GCM 加密明文，输出 base64(nonce||ciphertext)。
func (s *Store) encrypt(plain string) (string, error) {
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// decrypt 解密 base64(nonce||ciphertext)。
func (s *Store) decrypt(encoded string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("密文长度异常")
	}
	nonce, sealed := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, sealed, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
