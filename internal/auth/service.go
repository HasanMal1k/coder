package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/HasanMal1k/coder/internal/store"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// defaultSubscription is the tier every new user starts on. It is set here,
// never taken from the caller, so clients can't choose their own tier.
const defaultSubscription = "free"

// ErrInvalidCredentials is returned for both an unknown email and a wrong
// password, so callers can't tell which emails are registered.
var ErrInvalidCredentials = errors.New("invalid email or password")

// ErrEmailTaken is returned when Register is called with an email that
// already has an account.
var ErrEmailTaken = errors.New("email already registered")

// ErrInvalidToken is returned when ParseJWT is given a token that fails to
// parse, is expired, or was signed with an unexpected method.
var ErrInvalidToken = errors.New("invalid token")

type Service struct {
	userRepo  *store.UserRepository
	jwtSecret []byte
	// ttl is how long an access token is valid for. Zero means it never
	// expires.
	ttl time.Duration
}

// NewService returns an auth service whose access tokens last for ttl, or
// for ever if ttl is zero.
func NewService(userRepo *store.UserRepository, jwtSecret []byte, ttl time.Duration) *Service {
	return &Service{
		userRepo:  userRepo,
		jwtSecret: jwtSecret,
		ttl:       ttl,
	}
}

func (s *Service) Register(ctx context.Context, email, displayName, password, organization string) (string, *store.User, error) {
	hashPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", nil, err
	}

	user, err := s.userRepo.CreateUser(
		ctx,
		s.normalizeEmail(email),
		displayName,
		organization,
		defaultSubscription,
		string(hashPassword),
	)
	if err != nil {
		if errors.Is(err, store.ErrEmailTaken) {
			return "", nil, ErrEmailTaken
		}
		return "", nil, err
	}

	token, err := s.generateJWT(user)
	if err != nil {
		return "", nil, err
	}

	return token, user, nil
}

func (s *Service) Login(ctx context.Context, email, password string) (string, *store.User, error) {
	user, err := s.userRepo.GetUserByEmail(ctx, s.normalizeEmail(email))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return "", nil, ErrInvalidCredentials
		}
		return "", nil, err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			return "", nil, ErrInvalidCredentials
		}
		return "", nil, fmt.Errorf("compare password: %w", err)
	}

	token, err := s.generateJWT(user)
	if err != nil {
		return "", nil, err
	}

	return token, user, nil
}

// Renew issues a fresh access token for a user who has just presented a valid
// one. Where tokens expire, clients call it while they are in use, so a
// session lasts as long as the app stays open while each individual token
// stays short-lived. Where they don't, there is no need to.
func (s *Service) Renew(user *store.User) (string, error) {
	return s.generateJWT(user)
}

func (s *Service) normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func (s *Service) generateJWT(user *store.User) (string, error) {
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Subject:  user.ID,
		IssuedAt: jwt.NewNumericDate(now),
	}
	// A token with no expiry claim is one that doesn't expire: it stays
	// valid until the signing secret changes or its user is deleted.
	if s.ttl > 0 {
		claims.ExpiresAt = jwt.NewNumericDate(now.Add(s.ttl))
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(s.jwtSecret)
	if err != nil {
		return "", fmt.Errorf("sign token: %w", err)
	}

	return signed, nil
}

func (s *Service) ParseJWT(ctx context.Context, tokenString string) (*store.User, error) {
	token, err := jwt.ParseWithClaims(tokenString, &jwt.RegisteredClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return s.jwtSecret, nil
	})
	if err != nil {
		return nil, ErrInvalidToken
	}

	claims, ok := token.Claims.(*jwt.RegisteredClaims)
	if !ok || !token.Valid {
		return nil, ErrInvalidToken
	}

	user, err := s.userRepo.GetUserByID(ctx, claims.Subject)
	if err != nil {
		return nil, fmt.Errorf("get user by ID: %w", err)
	}

	return user, nil
}
