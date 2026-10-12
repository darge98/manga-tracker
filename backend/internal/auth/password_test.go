package auth

import (
	"encoding/hex"
	"testing"
)

func TestPBKDF2Vectors(t *testing.T) {
	for _, v := range []struct {
		iterations int
		expected   string
	}{{1, "120fb6cffcf8b32c43e7225256c4f837a86548c92ccc35480805987cb70be17b"}, {2, "ae4d0c95af6b46d32d0adff928f06dd02a303f8ef3c251dfd6e2d85a95474c43"}} {
		if got := hex.EncodeToString(derive([]byte("password"), []byte("salt"), v.iterations)); got != v.expected {
			t.Fatalf("PBKDF2 vector: %s", got)
		}
	}
}
func TestPasswordHashAndTokens(t *testing.T) {
	hash, e := Hash("a long password")
	if e != nil {
		t.Fatal(e)
	}
	if !Verify("a long password", hash) || Verify("wrong", hash) {
		t.Fatal("password verification")
	}
	if Verify("x", "pbkdf2-sha256$999999999$x$x") {
		t.Fatal("unsafe iterations accepted")
	}
	a, _ := Token()
	b, _ := Token()
	if a == b || len(a) != 64 || TokenHash(a) == a {
		t.Fatal("token randomness/hash")
	}
}
