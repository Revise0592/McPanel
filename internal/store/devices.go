package store

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"slices"
	"time"
)

// Device is a phone paired with the panel through the mobile app. It signs
// in with a token; only the token's hash is kept.
type Device struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	TokenHash string    `json:"tokenHash"`
	CreatedAt time.Time `json:"createdAt"`
	LastSeen  time.Time `json:"lastSeen"`
}

// seenEvery limits how often a device's last-seen time is written, so a
// busy app doesn't rewrite the settings file on every request.
const seenEvery = 5 * time.Minute

// HashToken returns the stored form of a device token.
func HashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// Devices returns the paired devices, newest first.
func (s *Store) Devices() []Device {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := slices.Clone(s.data.Devices)
	slices.SortFunc(out, func(a, b Device) int { return b.CreatedAt.Compare(a.CreatedAt) })
	return out
}

// AddDevice pairs a new device and returns its token, which is shown once.
func (s *Store) AddDevice(name string) (Device, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	token := "mcp_" + RandomHex(32)
	now := time.Now()
	d := Device{ID: RandomHex(6), Name: name, TokenHash: HashToken(token), CreatedAt: now, LastSeen: now}
	s.data.Devices = append(s.data.Devices, d)
	return d, token, s.saveLocked()
}

// DeviceByToken finds the device a token belongs to and notes that it was
// seen.
func (s *Store) DeviceByToken(token string) (Device, bool) {
	if token == "" {
		return Device{}, false
	}
	h := HashToken(token)
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.data.Devices {
		d := &s.data.Devices[i]
		if subtle.ConstantTimeCompare([]byte(d.TokenHash), []byte(h)) == 1 {
			if now := time.Now(); now.Sub(d.LastSeen) > seenEvery {
				d.LastSeen = now
				s.saveLocked()
			}
			return *d, true
		}
	}
	return Device{}, false
}

// RemoveDevice unpairs one device.
func (s *Store) RemoveDevice(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.data.Devices)
	s.data.Devices = slices.DeleteFunc(s.data.Devices, func(d Device) bool { return d.ID == id })
	if len(s.data.Devices) == n {
		return ErrNotFound
	}
	return s.saveLocked()
}

// RemoveAllDevices unpairs every device, as changing the password does.
func (s *Store) RemoveAllDevices() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.data.Devices) == 0 {
		return nil
	}
	s.data.Devices = nil
	return s.saveLocked()
}
