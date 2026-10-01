package portproxy

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	// ticketTTL is how long the link handed to the browser stays usable. It
	// only has to survive one navigation.
	ticketTTL = time.Minute
	// sessionTTL is how long the browser can use a machine's port before it
	// has to come back through the app for a new ticket.
	sessionTTL = 12 * time.Hour

	audienceTicket  = "workspace-ticket"
	audienceSession = "workspace-session"
)

var errInvalidGrant = errors.New("invalid or expired grant")

// target identifies one port on one machine.
type target struct {
	instanceID string
	port       int
}

type grantClaims struct {
	jwt.RegisteredClaims
	Instance string `json:"instance"`
	Port     int    `json:"port"`
}

// grants issues and checks the two tokens that let a browser reach a
// machine's port:
//
//   - a ticket: single-use and short-lived, carried in a URL from the app's
//     origin to the machine's origin;
//   - a session: longer-lived, kept in a cookie on the machine's origin.
//
// Both name one user, one instance and one port.
type grants struct {
	key []byte

	mu sync.Mutex
	// redeemed remembers tickets that have been used, until they expire, so
	// a ticket leaked through a URL can't be used a second time.
	redeemed map[string]time.Time
}

// newGrants derives its signing key from secret rather than using it
// directly. The same secret signs API access tokens; with a different key, a
// ticket or session can never be mistaken for one of those, or the reverse.
func newGrants(secret []byte) *grants {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte("coder workspace grants v1"))

	return &grants{
		key:      mac.Sum(nil),
		redeemed: make(map[string]time.Time),
	}
}

func (g *grants) issueTicket(userID string, t target) (string, error) {
	return g.sign(userID, t, audienceTicket, ticketTTL)
}

func (g *grants) issueSession(userID string, t target) (string, error) {
	return g.sign(userID, t, audienceSession, sessionTTL)
}

// redeemTicket checks a ticket for t and marks it used. It returns the user
// the ticket was issued to.
func (g *grants) redeemTicket(token string, t target) (string, error) {
	claims, err := g.verify(token, t, audienceTicket)
	if err != nil {
		return "", err
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	now := time.Now()
	for id, expiry := range g.redeemed {
		if now.After(expiry) {
			delete(g.redeemed, id)
		}
	}

	if _, used := g.redeemed[claims.ID]; used {
		return "", errInvalidGrant
	}
	g.redeemed[claims.ID] = claims.ExpiresAt.Time

	return claims.Subject, nil
}

func (g *grants) verifySession(token string, t target) error {
	_, err := g.verify(token, t, audienceSession)
	return err
}

func (g *grants) sign(userID string, t target, audience string, ttl time.Duration) (string, error) {
	now := time.Now()

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, grantClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			Audience:  jwt.ClaimStrings{audience},
			ID:        rand.Text(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
		Instance: t.instanceID,
		Port:     t.port,
	})

	return token.SignedString(g.key)
}

func (g *grants) verify(token string, t target, audience string) (*grantClaims, error) {
	var claims grantClaims

	_, err := jwt.ParseWithClaims(token, &claims,
		func(*jwt.Token) (any, error) { return g.key, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithAudience(audience),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return nil, errInvalidGrant
	}

	if claims.Instance != t.instanceID || claims.Port != t.port || claims.ID == "" {
		return nil, errInvalidGrant
	}

	return &claims, nil
}
