package main

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"strings"
	"sync"
	"time"
)

const (
	TOKEN_EXPIRATION_TIME = 5 * time.Minute
)

var (
	ErrNonexistentToken = errors.New("Token does not exist")
	ErrExpiredToken = errors.New("Token has expired")
)

type TokenManager struct {
	mu sync.Mutex
	activeTokens map[string]time.Time
}

func NewTokenManager() *TokenManager {
	return &TokenManager{
		activeTokens: make(map[string]time.Time),
	}
}

func (t *TokenManager) GenerateToken() (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	reader := io.LimitReader(rand.Reader, 64)

	var sb strings.Builder
	b64Enc := base64.NewEncoder(base64.URLEncoding, &sb)
	if _, err := io.Copy(b64Enc, reader); err != nil {
		return "", err
	}
	if err := b64Enc.Close(); err != nil {
		return "", err
	}
	return sb.String(), nil
}

func (t *TokenManager) ValidateToken(s string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	creationTime, ok := t.activeTokens[s]
	if !ok {
		return ErrNonexistentToken
	}

	if time.Now().Sub(creationTime) > TOKEN_EXPIRATION_TIME {
		delete(t.activeTokens, s)
		return ErrExpiredToken
	}

	delete(t.activeTokens, s)

	return nil
}

func (t *TokenManager) PruneTokens() {
	t.mu.Lock()
	defer t.mu.Unlock()

	var toPrune []string
	for t, ct := range t.activeTokens {
		if time.Now().Sub(ct) > TOKEN_EXPIRATION_TIME {
			toPrune = append(toPrune, t)
		}
	}

	for _, tp := range toPrune {
		delete(t.activeTokens, tp)
	}
}
