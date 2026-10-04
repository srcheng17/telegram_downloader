package adminauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"golang.org/x/crypto/bcrypt"
	"sync"
	"time"
	"unicode/utf8"
)

var (
	ErrUnauthenticated   = errors.New("authentication required")
	ErrInvalidPassword   = errors.New("invalid password")
	ErrBootstrapRequired = errors.New("administrator bootstrap required")
	ErrRateLimited       = errors.New("rate limited")
	ErrNotFound          = errors.New("not found")
)

const AbsoluteTTL = 12 * time.Hour
const IdleTTL = 30 * time.Minute
const PreauthTTL = 10 * time.Minute

type Account struct {
	PasswordHash      string
	CredentialVersion int64
}
type Session struct {
	TokenHash                      string
	CSRFToken                      string
	CredentialVersion              int64
	Preauth                        bool
	CreatedAt, LastSeen, ExpiresAt time.Time
}
type IssuedSession struct {
	Session
	Token string
}
type Repository interface {
	Account(context.Context) (Account, error)
	Bootstrap(context.Context, string) error
	CreateSession(context.Context, Session) error
	Session(context.Context, string, time.Time, bool) (Session, error)
	RotateSession(context.Context, string, Session) error
	DeleteSession(context.Context, string) error
	ChangePassword(context.Context, int64, string) error
}
type Options struct {
	BcryptCost int
	Now        func() time.Time
}
type Service struct {
	repo   Repository
	cost   int
	now    func() time.Time
	mu     sync.Mutex
	limits map[string]bucket
}
type bucket struct {
	count int
	until time.Time
}

func NewService(repo Repository, opts Options) *Service {
	if opts.BcryptCost == 0 {
		opts.BcryptCost = 12
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Service{repo: repo, cost: opts.BcryptCost, now: opts.Now, limits: map[string]bucket{}}
}
func ValidPassword(password string) bool {
	return utf8.ValidString(password) && utf8.RuneCountInString(password) >= 12 && len(password) <= 72
}
func (s *Service) Bootstrap(ctx context.Context, password string) error {
	if _, err := s.repo.Account(ctx); err == nil {
		return nil
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	if password == "" {
		return ErrBootstrapRequired
	}
	if !ValidPassword(password) {
		return ErrInvalidPassword
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), s.cost)
	if err != nil {
		return ErrInvalidPassword
	}
	return s.repo.Bootstrap(ctx, string(hash))
}
func Digest(token string) string { h := sha256.Sum256([]byte(token)); return hex.EncodeToString(h[:]) }
func (s *Service) issue(preauth bool, version int64) (IssuedSession, error) {
	raw := make([]byte, 64)
	if _, err := rand.Read(raw); err != nil {
		return IssuedSession{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw[:32])
	now := s.now()
	ttl := AbsoluteTTL
	if preauth {
		ttl = PreauthTTL
	}
	return IssuedSession{Token: token, Session: Session{TokenHash: Digest(token), CSRFToken: base64.RawURLEncoding.EncodeToString(raw[32:]), CredentialVersion: version, Preauth: preauth, CreatedAt: now, LastSeen: now, ExpiresAt: now.Add(ttl)}}, nil
}

// Limits use the actual peer address, never untrusted forwarded headers. Global
// counters and expiring bounded IP buckets prevent unbounded preauth/password work.
func (s *Service) allow(kind, peer string, perIP, total int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for k, b := range s.limits {
		if !now.Before(b.until) {
			delete(s.limits, k)
		}
	}
	keys := []string{kind + ":all", kind + ":" + peer}
	caps := []int{total, perIP}
	for i, k := range keys {
		b := s.limits[k]
		if b.count >= caps[i] {
			return false
		}
		if b.count == 0 && len(s.limits) >= 4096 {
			return false
		}
	}
	for _, k := range keys {
		b := s.limits[k]
		if b.count == 0 {
			b.until = now.Add(time.Minute)
		}
		b.count++
		s.limits[k] = b
	}
	return true
}
func (s *Service) Preauth(ctx context.Context, peer string) (IssuedSession, error) {
	if !s.allow("preauth", peer, 20, 200) {
		return IssuedSession{}, ErrRateLimited
	}
	if _, err := s.repo.Account(ctx); err != nil {
		return IssuedSession{}, ErrBootstrapRequired
	}
	issued, err := s.issue(true, 0)
	if err != nil {
		return issued, err
	}
	err = s.repo.CreateSession(ctx, issued.Session)
	return issued, err
}
func (s *Service) Validate(ctx context.Context, token string) (Session, error) {
	return s.validate(ctx, token, true)
}

// CheckSession revalidates a passive stream without extending its idle deadline.
func (s *Service) CheckSession(ctx context.Context, token string) (Session, error) {
	return s.validate(ctx, token, false)
}
func (s *Service) validate(ctx context.Context, token string, touchActivity bool) (Session, error) {
	if len(token) != 43 {
		return Session{}, ErrUnauthenticated
	}
	session, err := s.repo.Session(ctx, Digest(token), s.now(), touchActivity)
	if err != nil {
		return Session{}, err
	}
	now := s.now()
	if !now.Before(session.ExpiresAt) || !now.Before(session.LastSeen.Add(IdleTTL)) {
		return Session{}, ErrUnauthenticated
	}
	return session, nil
}
func ValidCSRF(session Session, csrf string) bool {
	return len(csrf) == 43 && subtle.ConstantTimeCompare([]byte(session.CSRFToken), []byte(csrf)) == 1
}
func (s *Service) Login(ctx context.Context, token, password, peer string) (IssuedSession, error) {
	if !s.allow("login", peer, 5, 30) {
		return IssuedSession{}, ErrRateLimited
	}
	old, err := s.Validate(ctx, token)
	if err != nil || !old.Preauth {
		return IssuedSession{}, ErrUnauthenticated
	}
	account, err := s.repo.Account(ctx)
	if err != nil {
		return IssuedSession{}, ErrUnauthenticated
	}
	if !ValidPassword(password) || bcrypt.CompareHashAndPassword([]byte(account.PasswordHash), []byte(password)) != nil {
		return IssuedSession{}, ErrUnauthenticated
	}
	issued, err := s.issue(false, account.CredentialVersion)
	if err != nil {
		return issued, err
	}
	err = s.repo.RotateSession(ctx, old.TokenHash, issued.Session)
	return issued, err
}
func (s *Service) Logout(ctx context.Context, token string) error {
	return s.repo.DeleteSession(ctx, Digest(token))
}
func (s *Service) ChangePassword(ctx context.Context, token, current, next, peer string) error {
	if !s.allow("login", peer, 5, 30) {
		return ErrRateLimited
	}
	session, err := s.Validate(ctx, token)
	if err != nil || session.Preauth {
		return ErrUnauthenticated
	}
	account, err := s.repo.Account(ctx)
	if err != nil {
		return err
	}
	if bcrypt.CompareHashAndPassword([]byte(account.PasswordHash), []byte(current)) != nil {
		return ErrUnauthenticated
	}
	if !ValidPassword(next) {
		return ErrInvalidPassword
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(next), s.cost)
	if err != nil {
		return ErrInvalidPassword
	}
	return s.repo.ChangePassword(ctx, session.CredentialVersion, string(hash))
}

// ResetPassword is restricted to the local maintenance command. It requires OS
// and database access, preserves the singleton account, and revokes every session.
// A concurrently changed password fails the repository's credential-version CAS.
func (s *Service) ResetPassword(ctx context.Context, password string) error {
	if !ValidPassword(password) {
		return ErrInvalidPassword
	}
	account, err := s.repo.Account(ctx)
	if err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), s.cost)
	if err != nil {
		return ErrInvalidPassword
	}
	return s.repo.ChangePassword(ctx, account.CredentialVersion, string(hash))
}
