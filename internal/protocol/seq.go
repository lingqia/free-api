package protocol

import "sync/atomic"

// seq 是 Responses 协议要求的全局递增事件序号。
var seq int64

// sequenceIncr 返回下一个事件序号。
func sequenceIncr() int64 { return atomic.AddInt64(&seq, 1) }
