package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/HasanMal1k/coder/internal/store"
)

// claimsOf reads back a token the service issued, checking its signature and
// whatever expiry it carries, the way ParseJWT does.
func claimsOf(t *testing.T, s *Service, token string) (*jwt.RegisteredClaims, error) {
	t.Helper()
	claims := &jwt.RegisteredClaims{}
	_, err := jwt.ParseWithClaims(token, claims, func(*jwt.Token) (any, error) { return s.jwtSecret, nil })
	return claims, err
}

func TestTokensThatNeverExpire(t *testing.T) {
	s := NewService(nil, []byte("secret"), 0)

	token, err := s.generateJWT(&store.User{ID: "user-1"})
	if err != nil {
		t.Fatalf("generateJWT: %v", err)
	}

	claims, err := claimsOf(t, s, token)
	if err != nil {
		t.Fatalf("a token with no expiry was rejected: %v", err)
	}
	if claims.ExpiresAt != nil {
		t.Errorf("expires at %v, want no expiry at all", claims.ExpiresAt)
	}
	if claims.Subject != "user-1" {
		t.Errorf("subject = %q", claims.Subject)
	}

	// It is still a signed token: one signed with another secret is refused.
	other := NewService(nil, []byte("another secret"), 0)
	if _, err := claimsOf(t, other, token); err == nil {
		t.Error("a token signed with a different secret was accepted")
	}
}

func TestTokensThatExpire(t *testing.T) {
	s := NewService(nil, []byte("secret"), 15*time.Minute)

	token, _ := s.generateJWT(&store.User{ID: "user-1"})
	claims, err := claimsOf(t, s, token)
	if err != nil {
		t.Fatalf("a fresh token was rejected: %v", err)
	}
	if left := time.Until(claims.ExpiresAt.Time); left < 14*time.Minute || left > 15*time.Minute {
		t.Errorf("expires in %s, want about 15 minutes", left)
	}

	// One that has run out is refused.
	expired := NewService(nil, []byte("secret"), time.Nanosecond)
	token, _ = expired.generateJWT(&store.User{ID: "user-1"})
	time.Sleep(1100 * time.Millisecond) // expiry is kept to the second
	if _, err := claimsOf(t, expired, token); err == nil {
		t.Error("an expired token was accepted")
	}
}
