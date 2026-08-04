package meta

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Token is a Meta OAuth access token and its lifecycle metadata.
type Token struct {
	AccessToken string    `json:"access_token"`
	TokenType   string    `json:"token_type,omitempty"`
	ExpiresAt   time.Time `json:"expires_at,omitempty"`
	Scopes      []Scope   `json:"scopes,omitempty"`
}

func (t Token) Valid(now time.Time) bool {
	return t.AccessToken != "" && (t.ExpiresAt.IsZero() || now.Before(t.ExpiresAt))
}

// String is redacted so accidental formatting never writes a credential.
func (Token) String() string { return "meta.Token{AccessToken:[REDACTED]}" }

// TokenStore persists OAuth tokens. Implementations must be safe for
// concurrent use and should encrypt credentials at rest.
type TokenStore interface {
	Load(context.Context) (Token, error)
	Save(context.Context, Token) error
}

// MemoryTokenStore is a concurrency-safe store for tests and short-lived
// processes. Production applications should provide durable encrypted storage.
type MemoryTokenStore struct {
	mu    sync.RWMutex
	token Token
}

func NewMemoryTokenStore(token Token) *MemoryTokenStore {
	return &MemoryTokenStore{token: cloneToken(token)}
}

func (s *MemoryTokenStore) Load(_ context.Context) (Token, error) {
	if s == nil {
		return Token{}, fmt.Errorf("%w: nil token store", ErrTokenMissing)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneToken(s.token), nil
}

func (s *MemoryTokenStore) Save(_ context.Context, token Token) error {
	if s == nil {
		return fmt.Errorf("%w: nil token store", ErrTokenMissing)
	}
	s.mu.Lock()
	s.token = cloneToken(token)
	s.mu.Unlock()
	return nil
}

func cloneToken(token Token) Token {
	token.Scopes = append([]Scope(nil), token.Scopes...)
	return token
}
