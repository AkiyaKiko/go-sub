package lib

import (
	"crypto/rand"
)

const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

func RandomString(n int) (string, error) {
	b := make([]byte, n)
	letterLen := byte(len(letters))

	randomBytes := make([]byte, n)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}

	for i := range n {
		b[i] = letters[randomBytes[i]%letterLen]
	}

	return string(b), nil
}
