package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/HasanMal1k/coder/internal/auth/model"
	"github.com/HasanMal1k/coder/internal/auth/repository"
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

type UserService struct {
	userRepo  *repository.UserRepository
	jwtSecret []byte
	ttl       time.Duration
}

func NewUserService(userRepo *repository.UserRepository, jwtSecret []byte, ttl time.Duration) *UserService {
	return &UserService{
		userRepo:  userRepo,
		jwtSecret: jwtSecret,
		ttl:       ttl,
	}
}

func (s *UserService) Register(ctx context.Context, email, displayName, password, organization string) (string, *model.User, error) {
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
		if errors.Is(err, repository.ErrEmailTaken) {
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

func (s *UserService) Login(ctx context.Context, email, password string) (string, *model.User, error) {
	user, err := s.userRepo.GetUserByEmail(ctx, s.normalizeEmail(email))
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
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

func (s *UserService) normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func (s *UserService) generateJWT(user *model.User) (string, error) {
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Subject:   user.ID,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(s.ttl)),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(s.jwtSecret)
	if err != nil {
		return "", fmt.Errorf("sign token: %w", err)
	}

	return signed, nil
}

func (s *UserService) ParseJWT(ctx context.Context, tokenString string) (*model.User, error) {
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
