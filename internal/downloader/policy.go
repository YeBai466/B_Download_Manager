package downloader

import (
	"context"
	"sync"
	"time"
)

const (
	defaultRetries      = 3
	defaultStallTimeout = 30 * time.Second
	defaultMetaInterval = 2 * time.Second
	minChunkSize        = int64(1 << 20)   // 1 MiB
	defaultChunkSize    = int64(8 << 20)   // 8 MiB
	largeFileChunkSize  = int64(16 << 20)  // 16 MiB
	smallFileThreshold  = int64(8 << 20)   // 8 MiB
	mediumFileThreshold = int64(64 << 20)  // 64 MiB
	largeFileThreshold  = int64(512 << 20) // 512 MiB
)

type RuntimeConfig struct {
	MaxConcurrent  int
	MaxConnections int
	SpeedLimit     int64
	Retries        int
	StallTimeout   time.Duration
	MetaInterval   time.Duration
}

func normalizeRuntimeConfig(c RuntimeConfig) RuntimeConfig {
	if c.MaxConcurrent < 1 {
		c.MaxConcurrent = 5
	}
	if c.Retries < 0 {
		c.Retries = defaultRetries
	}
	if c.Retries == 0 {
		c.Retries = defaultRetries
	}
	if c.StallTimeout <= 0 {
		c.StallTimeout = defaultStallTimeout
	}
	if c.MetaInterval <= 0 {
		c.MetaInterval = defaultMetaInterval
	}
	if c.MaxConnections < 1 {
		c.MaxConnections = c.MaxConcurrent * DefaultConnections
	}
	return c
}

func smartConnections(total int64, requested int) int {
	if requested < 1 {
		requested = DefaultConnections
	}
	if total <= 0 {
		return 1
	}
	switch {
	case total < smallFileThreshold:
		return 1
	case total < mediumFileThreshold:
		return minInt(requested, 2)
	case total < largeFileThreshold:
		return minInt(requested, 4)
	default:
		return requested
	}
}

func smartChunkSize(total int64) int64 {
	switch {
	case total <= 0:
		return defaultChunkSize
	case total < mediumFileThreshold:
		return minChunkSize
	case total < largeFileThreshold:
		return defaultChunkSize
	default:
		return largeFileChunkSize
	}
}

type speedLimiter struct {
	mu        sync.Mutex
	rate      int64
	updated   time.Time
	allowance float64
}

func newSpeedLimiter(rate int64) *speedLimiter {
	return &speedLimiter{rate: rate, updated: time.Now()}
}

func (l *speedLimiter) SetRate(rate int64) {
	l.mu.Lock()
	l.rate = rate
	l.updated = time.Now()
	if rate <= 0 {
		l.allowance = 0
	} else if l.allowance > float64(rate) {
		l.allowance = float64(rate)
	}
	l.mu.Unlock()
}

func (l *speedLimiter) Wait(ctx context.Context, n int) error {
	if l == nil || n <= 0 {
		return ctx.Err()
	}
	for {
		l.mu.Lock()
		rate := l.rate
		if rate <= 0 {
			l.mu.Unlock()
			return ctx.Err()
		}
		now := time.Now()
		elapsed := now.Sub(l.updated).Seconds()
		l.updated = now
		l.allowance += elapsed * float64(rate)
		if cap := float64(rate); l.allowance > cap {
			l.allowance = cap
		}
		if l.allowance >= float64(n) {
			l.allowance -= float64(n)
			l.mu.Unlock()
			return ctx.Err()
		}
		need := float64(n) - l.allowance
		wait := time.Duration(need / float64(rate) * float64(time.Second))
		if wait < 10*time.Millisecond {
			wait = 10 * time.Millisecond
		}
		l.mu.Unlock()

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

type connectionLimiter struct {
	ch chan struct{}
}

func newConnectionLimiter(n int) *connectionLimiter {
	if n < 1 {
		n = DefaultConnections
	}
	return &connectionLimiter{ch: make(chan struct{}, n)}
}

func (l *connectionLimiter) Resize(n int) {
	if n < 1 {
		n = DefaultConnections
	}
	l.ch = make(chan struct{}, n)
}

func (l *connectionLimiter) Acquire(ctx context.Context) error {
	if l == nil {
		return ctx.Err()
	}
	select {
	case l.ch <- struct{}{}:
		return ctx.Err()
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (l *connectionLimiter) Release() {
	if l == nil {
		return
	}
	select {
	case <-l.ch:
	default:
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
