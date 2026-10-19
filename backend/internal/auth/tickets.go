package auth

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

type TicketStore struct {
	mu      sync.Mutex
	tickets map[string]ticketEntry
}

type ticketEntry struct {
	claims    *Claims
	expiresAt time.Time
}

var DefaultTicketStore = &TicketStore{
	tickets: make(map[string]ticketEntry),
}

// CreateTicket creates a single-use token tied to user claims with a short TTL
func (s *TicketStore) CreateTicket(claims *Claims, ttl time.Duration) (string, error) {
	bytes := make([]byte, 24)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	ticket := hex.EncodeToString(bytes)

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	// Periodic cleanup of expired tickets
	for k, v := range s.tickets {
		if now.After(v.expiresAt) {
			delete(s.tickets, k)
		}
	}

	s.tickets[ticket] = ticketEntry{
		claims:    claims,
		expiresAt: now.Add(ttl),
	}
	return ticket, nil
}

// ConsumeTicket validates and removes the single-use ticket, returning claims if valid
func (s *TicketStore) ConsumeTicket(ticket string) *Claims {
	if ticket == "" {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	entry, exists := s.tickets[ticket]
	if !exists {
		return nil
	}

	// Single-use: immediately delete
	delete(s.tickets, ticket)

	if time.Now().After(entry.expiresAt) {
		return nil
	}
	return entry.claims
}
