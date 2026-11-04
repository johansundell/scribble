package auth

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// TOTP login codes (RFC 6238, ADR 13): HMAC-SHA1, 30-second steps, 6
// digits, as Google Authenticator and compatible apps make them.
const (
	totpStep   = 30 * time.Second
	totpDigits = 6
	totpModulo = 1_000_000 // 10^totpDigits
	// totpDrift is how many steps either side of now a code may be from,
	// for a phone whose clock is a little off.
	totpDrift = 1
	// MinSecretBytes is the shortest WEB_TOTP_SECRET accepted: 128 bits,
	// the minimum of RFC 4226.
	MinSecretBytes = 16
)

// secretEncoding is base32 without padding, as authenticator apps use it.
var secretEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// DecodeSecret decodes a base32 TOTP secret, ignoring case, spaces and
// padding, as authenticator apps show it. It must be at least
// MinSecretBytes long.
func DecodeSecret(s string) ([]byte, error) {
	s = strings.ToUpper(strings.TrimRight(strings.Join(strings.Fields(s), ""), "="))
	key, err := secretEncoding.DecodeString(s)
	if err != nil {
		return nil, errors.New("not base32")
	}
	if len(key) < MinSecretBytes {
		return nil, fmt.Errorf("shorter than %d bytes (%d base32 characters)", MinSecretBytes, secretEncoding.EncodedLen(MinSecretBytes))
	}
	return key, nil
}

// EncodeSecret is key as base32 without padding, as authenticator apps
// take it.
func EncodeSecret(key []byte) string {
	return secretEncoding.EncodeToString(key)
}

// Code returns the 6-digit code of key for the 30-second step holding t.
func Code(key []byte, t time.Time) string {
	return codeAt(key, stepOf(t))
}

// stepOf is the number of the 30-second step holding t.
func stepOf(t time.Time) int64 {
	return t.Unix() / int64(totpStep/time.Second)
}

// codeAt is HOTP (RFC 4226) of key for counter step.
func codeAt(key []byte, step int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step))
	m := hmac.New(sha1.New, key)
	m.Write(msg[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	n := binary.BigEndian.Uint32(sum[off:]) & 0x7fffffff
	return fmt.Sprintf("%0*d", totpDigits, n%totpModulo)
}

// totp checks codes for one key and remembers the latest step a login used,
// so no code of that step or an earlier one logs in again.
type totp struct {
	key  []byte
	mu   sync.Mutex
	used int64
}

func newTOTP(key []byte) *totp {
	return &totp{key: append([]byte(nil), key...), used: -1}
}

// match returns the step within the drift of now whose code is code.
func (o *totp) match(code string, now time.Time) (int64, bool) {
	if len(code) != totpDigits {
		return 0, false
	}
	step := stepOf(now)
	found, ok := int64(0), false
	// Check every step, without stopping early, so the time taken doesn't
	// tell which one matched.
	for s := step - totpDrift; s <= step+totpDrift; s++ {
		if subtle.ConstantTimeCompare([]byte(codeAt(o.key, s)), []byte(code)) == 1 {
			found, ok = s, true
		}
	}
	return found, ok
}

// check reports whether code is right at now, without using it up.
func (o *totp) check(code string, now time.Time) bool {
	_, ok := o.match(code, now)
	return ok
}

// use reports whether code is right at now and its step newer than the last
// one used, and if so marks it used.
func (o *totp) use(code string, now time.Time) bool {
	step, ok := o.match(code, now)
	if !ok {
		return false
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if step <= o.used {
		return false
	}
	o.used = step
	return true
}
