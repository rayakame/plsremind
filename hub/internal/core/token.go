package core

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

const (
	PrefixSession    = "plsr_ses_"
	PrefixPersonal   = "plsr_pat_"
	PrefixConnection = "plsr_con_"
	PrefixInvite     = "plsr_inv_"
)

// NewToken returns a random token and the hash to store for it.
func NewToken(prefix string) (plain string, hash []byte) {
	raw := make([]byte, 32)
	_, _ = rand.Read(raw)
	plain = prefix + base64.RawURLEncoding.EncodeToString(raw)
	return plain, HashToken(plain)
}

func HashToken(plain string) []byte {
	sum := sha256.Sum256([]byte(plain))
	return sum[:]
}
