package protocol

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// randomID 生成 8 字节随机 ID。
func randomID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// nowUnix 返回当前 Unix 时间戳（秒）。
func nowUnix() int64 { return time.Now().Unix() }
