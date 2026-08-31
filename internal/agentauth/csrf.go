package agentauth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"sync"
	"time"
)

var (
	ErrInvalidCSRF  = errors.New("CSRF token is invalid or expired")
	ErrCSRFCapacity = errors.New("CSRF subject capacity is exhausted")
)

const (
	DefaultCSRFTTL           = 15 * time.Minute
	DefaultCSRFRefreshWindow = 5 * time.Minute
	DefaultCSRFOverlap       = 30 * time.Second
	DefaultCSRFMaxEntries    = 4096
)

type CSRFToken struct {
	Value     string
	ExpiresAt time.Time
	RefreshAt time.Time
	Rotated   bool
}

type csrfSlot struct {
	currentHash    [sha256.Size]byte
	currentExpires time.Time
	previousHash   [sha256.Size]byte
	previousUntil  time.Time
}

// CSRFManager stores only token hashes and keeps credentials in memory. A
// refresh rotates tokens during the last five minutes and accepts the previous
// token for a thirty-second overlap by default.
type CSRFManager struct {
	mu            sync.Mutex
	values        map[string]csrfSlot
	TTL           time.Duration
	RefreshWindow time.Duration
	Overlap       time.Duration
	MaxEntries    int
	Clock         func() time.Time
}

func NewCSRFManager() *CSRFManager {
	return &CSRFManager{
		values: make(map[string]csrfSlot), TTL: DefaultCSRFTTL,
		RefreshWindow: DefaultCSRFRefreshWindow, Overlap: DefaultCSRFOverlap,
		MaxEntries: DefaultCSRFMaxEntries, Clock: time.Now,
	}
}

func (m *CSRFManager) Issue(subject string) (CSRFToken, error) {
	value, hash, err := newCSRFValue()
	if err != nil {
		return CSRFToken{}, err
	}
	now := m.now()
	m.mu.Lock()
	m.ensure()
	_, exists := m.values[subject]
	if !exists && len(m.values) >= m.maxEntries() {
		m.cleanupExpiredLocked(now)
	}
	if _, exists = m.values[subject]; !exists && len(m.values) >= m.maxEntries() {
		m.mu.Unlock()
		return CSRFToken{}, ErrCSRFCapacity
	}
	m.values[subject] = csrfSlot{currentHash: hash, currentExpires: now.Add(m.ttl())}
	slot := m.values[subject]
	m.mu.Unlock()
	return m.token(value, slot.currentExpires, true), nil
}

func (m *CSRFManager) Validate(subject, value string) error {
	hash := sha256.Sum256([]byte(value))
	now := m.now()
	m.mu.Lock()
	slot, ok := m.values[subject]
	if ok && !now.Before(slot.currentExpires) {
		delete(m.values, subject)
		ok = false
	}
	m.mu.Unlock()
	if !ok {
		return ErrInvalidCSRF
	}
	current := now.Before(slot.currentExpires) && equalHash(hash, slot.currentHash)
	previous := now.Before(slot.previousUntil) && equalHash(hash, slot.previousHash)
	if !current && !previous {
		return ErrInvalidCSRF
	}
	return nil
}

func (m *CSRFManager) Refresh(subject, value string) (CSRFToken, error) {
	now := m.now()
	hash := sha256.Sum256([]byte(value))
	m.mu.Lock()
	m.ensure()
	slot, ok := m.values[subject]
	if !ok || !equalHash(hash, slot.currentHash) || !now.Before(slot.currentExpires) {
		m.mu.Unlock()
		return CSRFToken{}, ErrInvalidCSRF
	}
	if slot.currentExpires.Sub(now) > m.refreshWindow() {
		m.mu.Unlock()
		return m.token(value, slot.currentExpires, false), nil
	}
	newValue, newHash, err := newCSRFValue()
	if err != nil {
		m.mu.Unlock()
		return CSRFToken{}, err
	}
	slot.previousHash = slot.currentHash
	slot.previousUntil = now.Add(m.overlap())
	if slot.previousUntil.After(slot.currentExpires) {
		slot.previousUntil = slot.currentExpires
	}
	slot.currentHash = newHash
	slot.currentExpires = now.Add(m.ttl())
	m.values[subject] = slot
	m.mu.Unlock()
	return m.token(newValue, slot.currentExpires, true), nil
}

func (m *CSRFManager) Delete(subject string) {
	m.mu.Lock()
	delete(m.values, subject)
	m.mu.Unlock()
}

func (m *CSRFManager) token(value string, expires time.Time, rotated bool) CSRFToken {
	return CSRFToken{
		Value: value, ExpiresAt: expires,
		RefreshAt: expires.Add(-m.refreshWindow()), Rotated: rotated,
	}
}

func (m *CSRFManager) ensure() {
	if m.values == nil {
		m.values = make(map[string]csrfSlot)
	}
}

func (m *CSRFManager) now() time.Time {
	if m.Clock != nil {
		return m.Clock()
	}
	return time.Now()
}

func (m *CSRFManager) ttl() time.Duration {
	if m.TTL > 0 {
		return m.TTL
	}
	return DefaultCSRFTTL
}

func (m *CSRFManager) refreshWindow() time.Duration {
	if m.RefreshWindow > 0 && m.RefreshWindow < m.ttl() {
		return m.RefreshWindow
	}
	return DefaultCSRFRefreshWindow
}

func (m *CSRFManager) overlap() time.Duration {
	if m.Overlap > 0 {
		return m.Overlap
	}
	return DefaultCSRFOverlap
}

func (m *CSRFManager) maxEntries() int {
	if m.MaxEntries > 0 {
		return m.MaxEntries
	}
	return DefaultCSRFMaxEntries
}

func (m *CSRFManager) cleanupExpiredLocked(now time.Time) {
	for subject, slot := range m.values {
		if !now.Before(slot.currentExpires) {
			delete(m.values, subject)
		}
	}
}

func newCSRFValue() (string, [sha256.Size]byte, error) {
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", [sha256.Size]byte{}, err
	}
	value := base64.RawURLEncoding.EncodeToString(random[:])
	return value, sha256.Sum256([]byte(value)), nil
}

func equalHash(left, right [sha256.Size]byte) bool {
	return subtle.ConstantTimeCompare(left[:], right[:]) == 1
}
