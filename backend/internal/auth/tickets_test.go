package auth

import (
	"testing"
	"time"
)

func TestTicketStore(t *testing.T) {
	store := &TicketStore{
		tickets: make(map[string]ticketEntry),
	}

	testClaims := &Claims{
		UserID:   42,
		Username: "alice",
		Role:     "admin",
	}

	// 1. Create ticket
	ticket, err := store.CreateTicket(testClaims, 2*time.Second)
	if err != nil {
		t.Fatalf("unexpected error creating ticket: %v", err)
	}
	if ticket == "" {
		t.Fatal("expected non-empty ticket")
	}

	// 2. Consume ticket once
	claims := store.ConsumeTicket(ticket)
	if claims == nil {
		t.Fatal("expected valid claims from ticket")
	}
	if claims.Username != "alice" || claims.UserID != 42 {
		t.Fatalf("unexpected claims: %+v", claims)
	}

	// 3. Consume again (must fail due to single-use)
	claimsSecond := store.ConsumeTicket(ticket)
	if claimsSecond != nil {
		t.Fatal("expected ticket to be invalidated after single use")
	}

	// 4. Test expiration
	expiredTicket, err := store.CreateTicket(testClaims, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error creating ticket: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	claimsExpired := store.ConsumeTicket(expiredTicket)
	if claimsExpired != nil {
		t.Fatal("expected ticket to be expired")
	}
}
