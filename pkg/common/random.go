package common

import (
	"math/rand"
	"time"
)

const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

var localRand = rand.New(rand.NewSource(time.Now().UnixNano()))

func RandomStr(length int) string {
	localRand.Seed(time.Now().UnixNano())
	b := make([]byte, length)
	for i := range b {
		b[i] = letters[localRand.Intn(len(letters))]
	}
	return string(b)
}
