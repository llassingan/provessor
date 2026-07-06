package repository

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
)

const credentialsCallbackTokenBytes = 32

func GenerateCredentialsCallbackToken() (string, error) {
	token := make([]byte, credentialsCallbackTokenBytes)
	if _, err := rand.Read(token); err != nil {
		return "", err
	}
	return hex.EncodeToString(token), nil
}

func HashCredentialsCallbackToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
