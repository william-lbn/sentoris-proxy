package storage

import (
	"context"
	"log"
	"time"
)

type TTLCleaner struct {
	traceStore    TraceStore
	cleanupTicker *time.Ticker
	cleanupInterval time.Duration
}

func NewTTLCleaner(traceStore TraceStore, cleanupInterval time.Duration) *TTLCleaner {
	return &TTLCleaner{
		traceStore:    traceStore,
		cleanupInterval: cleanupInterval,
	}
}

func (c *TTLCleaner) Start(ctx context.Context) {
	if c.cleanupTicker == nil {
		c.cleanupTicker = time.NewTicker(c.cleanupInterval)
	}

	go func() {
		for {
			select {
			case <-c.cleanupTicker.C:
				c.cleanupExpiredTraces(ctx)
			case <-ctx.Done():
				c.Stop()
				return
			}
		}
	}()
}

func (c *TTLCleaner) Stop() {
	if c.cleanupTicker != nil {
		c.cleanupTicker.Stop()
		c.cleanupTicker = nil
	}
}

func (c *TTLCleaner) cleanupExpiredTraces(ctx context.Context) {
	log.Println("Starting TTL cleanup for expired traces")
	
	deletedCount, err := c.traceStore.CleanupExpired(ctx)
	if err != nil {
		log.Printf("Failed to cleanup expired traces: %v", err)
		return
	}
	
	if deletedCount > 0 {
		log.Printf("Successfully deleted %d expired traces", deletedCount)
	} else {
		log.Println("No expired traces found")
	}
}
