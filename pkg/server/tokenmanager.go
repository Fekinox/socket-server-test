package server

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"strings"
	"sync"
	"time"
)

type TokenPayload struct {
	CreationTime time.Time
	Username     string
}

const (
	TOKEN_EXPIRATION_TIME  = 5 * time.Minute
	MAX_TOKEN_GEN_ATTEMPTS = 5
	TOKEN_LENGTH           = 16
)

var (
	ErrNonexistentToken = errors.New("Token does not exist")
	ErrExpiredToken     = errors.New("Token has expired")
	ErrTooManyAttempts  = errors.New("Token generation failed after too many attempts")
)

type TokenManager struct {
	mu           sync.Mutex
	activeTokens map[string]TokenPayload
}

func NewTokenManager() *TokenManager {
	return &TokenManager{
		activeTokens: make(map[string]TokenPayload),
	}
}

func (t *TokenManager) GenerateToken(username string) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	var attempts int
	var tok string

	for {
		if tok != "" {
			if _, ok := t.activeTokens[tok]; !ok {
				break
			}
		}

		if attempts > MAX_TOKEN_GEN_ATTEMPTS {
			return "", ErrTooManyAttempts
		}
		reader := io.LimitReader(rand.Reader, TOKEN_LENGTH)

		var sb strings.Builder
		b64Enc := base64.NewEncoder(base64.URLEncoding, &sb)
		if _, err := io.Copy(b64Enc, reader); err != nil {
			return "", err
		}
		if err := b64Enc.Close(); err != nil {
			return "", err
		}

		tok = sb.String()
	}

	t.activeTokens[tok] = TokenPayload{
		CreationTime: time.Now(),
		Username:     username,
	}

	return tok, nil
}

func (t *TokenManager) ValidateToken(s string) (TokenPayload, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	payload, ok := t.activeTokens[s]
	if !ok {
		return TokenPayload{}, ErrNonexistentToken
	}

	if time.Now().Sub(payload.CreationTime) > TOKEN_EXPIRATION_TIME {
		delete(t.activeTokens, s)
		return TokenPayload{}, ErrExpiredToken
	}

	delete(t.activeTokens, s)

	return payload, nil
}

func (t *TokenManager) PruneTokens() {
	t.mu.Lock()
	defer t.mu.Unlock()

	var toPrune []string
	for t, p := range t.activeTokens {
		if time.Now().Sub(p.CreationTime) > TOKEN_EXPIRATION_TIME {
			toPrune = append(toPrune, t)
		}
	}

	for _, tp := range toPrune {
		delete(t.activeTokens, tp)
	}
}
