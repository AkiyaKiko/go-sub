package lib

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"math/big"
)

const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
const publicPathRandomLength = 16

func RandomString(n int) (string, error) {
	if n < 0 {
		return "", fmt.Errorf("random string length must not be negative")
	}
	b := make([]byte, n)
	max := big.NewInt(int64(len(letters)))
	for i := range n {
		index, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		b[i] = letters[index.Int64()]
	}

	return string(b), nil
}

func RandomPublicPath() (string, error) {
	random, err := RandomString(publicPathRandomLength)
	if err != nil {
		return "", err
	}
	return "/" + random, nil
}

func RandUserID() (int64, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0, err
	}
	return int64(binary.LittleEndian.Uint64(b[:])), nil
}
