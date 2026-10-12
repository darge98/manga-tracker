package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const Iterations = 600000

// derive is PBKDF2-HMAC-SHA256 (RFC 8018), restricted to one 32-byte block.
// The implementation is independently checked against published vectors and Python hashlib.
func derive(password, salt []byte, iterations int) []byte {
	mac := hmac.New(sha256.New, password)
	mac.Write(salt)
	var block [4]byte
	binary.BigEndian.PutUint32(block[:], 1)
	mac.Write(block[:])
	u := mac.Sum(nil)
	out := append([]byte(nil), u...)
	for i := 1; i < iterations; i++ {
		mac.Reset()
		mac.Write(u)
		u = mac.Sum(u[:0])
		for j := range out {
			out[j] ^= u[j]
		}
	}
	return out
}
func Hash(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", Iterations, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(derive([]byte(password), salt, Iterations))), nil
}
func Verify(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	n, err := strconv.Atoi(parts[1])
	if err != nil || n < 10000 || n > 2000000 {
		return false
	}
	salt, e1 := base64.RawStdEncoding.DecodeString(parts[2])
	expected, e2 := base64.RawStdEncoding.DecodeString(parts[3])
	if e1 != nil || e2 != nil || len(salt) != 16 || len(expected) != 32 {
		return false
	}
	return subtle.ConstantTimeCompare(derive([]byte(password), salt, n), expected) == 1
}
func Token() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
func TokenHash(token string) string {
	b := sha256.Sum256([]byte(token))
	return hex.EncodeToString(b[:])
}

var ErrCredentials = errors.New("invalid credentials")
