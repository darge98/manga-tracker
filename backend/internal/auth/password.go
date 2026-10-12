package auth

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

const Iterations = 600000

// derive is PBKDF2-HMAC-SHA256 (RFC 8018) with a 32-byte key. The stored hash
// format predates crypto/pbkdf2, which produces identical output.
func derive(password string, salt []byte, iterations int) ([]byte, error) {
	return pbkdf2.Key(sha256.New, password, salt, iterations, 32)
}

func Hash(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := derive(password, salt, Iterations)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", Iterations, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
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
	key, err := derive(password, salt, n)
	return err == nil && subtle.ConstantTimeCompare(key, expected) == 1
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
