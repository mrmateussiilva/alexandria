package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"alexandria/internal/config"
)

const (
	CookieName = "alexandria_session"
	sessionTTL = 7 * 24 * time.Hour
)

var ErrInvalidCredentials = errors.New("invalid credentials")

type Service struct {
	enabled      bool
	username     string
	passwordHash [sha256.Size]byte
	secret       []byte
}

func New(cfg config.AuthConfig) (*Service, error) {
	service := &Service{enabled: cfg.Enabled}
	if !cfg.Enabled {
		return service, nil
	}
	service.username = strings.TrimSpace(cfg.Username)
	if service.username == "" {
		return nil, fmt.Errorf("auth username is required")
	}

	if cfg.PasswordHash != "" {
		decoded, err := hex.DecodeString(strings.TrimSpace(cfg.PasswordHash))
		if err != nil || len(decoded) != sha256.Size {
			return nil, fmt.Errorf("auth password hash must be a SHA-256 hex string")
		}
		copy(service.passwordHash[:], decoded)
	} else {
		service.passwordHash = sha256.Sum256([]byte(cfg.Password))
	}

	secret := strings.TrimSpace(cfg.Secret)
	if secret == "" {
		random := make([]byte, 32)
		if _, err := rand.Read(random); err != nil {
			return nil, fmt.Errorf("generate auth secret: %w", err)
		}
		service.secret = random
	} else {
		service.secret = []byte(secret)
	}
	return service, nil
}

func (s *Service) Enabled() bool {
	return s != nil && s.enabled
}

func (s *Service) Username() string {
	if s == nil {
		return ""
	}
	return s.username
}

func (s *Service) Authenticate(username, password string) error {
	if !s.Enabled() {
		return nil
	}
	if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(username)), []byte(s.username)) != 1 {
		return ErrInvalidCredentials
	}
	hash := sha256.Sum256([]byte(password))
	if subtle.ConstantTimeCompare(hash[:], s.passwordHash[:]) != 1 {
		return ErrInvalidCredentials
	}
	return nil
}

func (s *Service) NewCookie(r *http.Request) (*http.Cookie, error) {
	if !s.Enabled() {
		return nil, nil
	}
	expires := time.Now().Add(sessionTTL).Unix()
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate session nonce: %w", err)
	}
	payload := s.username + "|" + strconv.FormatInt(expires, 10) + "|" + base64.RawURLEncoding.EncodeToString(nonce)
	signature := s.sign(payload)
	return &http.Cookie{
		Name:     CookieName,
		Value:    base64.RawURLEncoding.EncodeToString([]byte(payload + "|" + signature)),
		Path:     "/",
		MaxAge:   int(sessionTTL.Seconds()),
		Expires:  time.Unix(expires, 0),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isSecureRequest(r),
	}, nil
}

func (s *Service) ClearCookie(r *http.Request) *http.Cookie {
	return &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isSecureRequest(r),
	}
}

func (s *Service) Authenticated(r *http.Request) bool {
	if !s.Enabled() {
		return true
	}
	cookie, err := r.Cookie(CookieName)
	if err != nil || cookie.Value == "" {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(cookie.Value)
	if err != nil {
		return false
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != 4 {
		return false
	}
	if subtle.ConstantTimeCompare([]byte(parts[0]), []byte(s.username)) != 1 {
		return false
	}
	expires, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || time.Now().Unix() > expires {
		return false
	}
	payload := strings.Join(parts[:3], "|")
	expected := s.sign(payload)
	return subtle.ConstantTimeCompare([]byte(parts[3]), []byte(expected)) == 1
}

func (s *Service) sign(payload string) string {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func isSecureRequest(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}
