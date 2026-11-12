package sidecarauth

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"time"
)

const (
	MaxTimestampSkewSeconds = 30
	ReplayRetentionDuration = 60 * time.Second
)

var (
	ErrReplayDetected       = errors.New("request_id or nonce has already been processed")
	ErrTimestampSkewTooHigh = errors.New("timestamp skew exceeds allowable 30-second window")
)

type replayItem struct {
	expiresAt time.Time
}

// ReplayCache tracks used request IDs and nonces to protect against replay attacks
type ReplayCache struct {
	mu    sync.Mutex
	items map[string]replayItem
}

// NewReplayCache creates an initialized ReplayCache
func NewReplayCache() *ReplayCache {
	rc := &ReplayCache{
		items: make(map[string]replayItem),
	}
	go rc.cleanupLoop()
	return rc
}

func (c *ReplayCache) cleanupLoop() {
	ticker := time.NewTicker(30 * time.Second)
	for range ticker.C {
		c.mu.Lock()
		now := time.Now()
		for k, v := range c.items {
			if now.After(v.expiresAt) {
				delete(c.items, k)
			}
		}
		c.mu.Unlock()
	}
}

// CheckAndRecord verifies that the timestamp is within window and that request_id and nonce have not been seen
func (c *ReplayCache) CheckAndRecord(installationID, requestID, nonce string, requestTimestamp time.Time, now time.Time) error {
	// 1. Verify timestamp difference is within 30 seconds
	diff := math.Abs(now.Sub(requestTimestamp).Seconds())
	if diff > float64(MaxTimestampSkewSeconds) {
		return fmt.Errorf("%w: difference is %.1fs (max allowed: %ds)", ErrTimestampSkewTooHigh, diff, MaxTimestampSkewSeconds)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	reqKey := fmt.Sprintf("req:%s:%s", installationID, requestID)
	nonceKey := fmt.Sprintf("nonce:%s:%s", installationID, nonce)

	if item, exists := c.items[reqKey]; exists && now.Before(item.expiresAt) {
		return ErrReplayDetected
	}
	if item, exists := c.items[nonceKey]; exists && now.Before(item.expiresAt) {
		return ErrReplayDetected
	}

	exp := now.Add(ReplayRetentionDuration)
	c.items[reqKey] = replayItem{expiresAt: exp}
	c.items[nonceKey] = replayItem{expiresAt: exp}

	return nil
}
