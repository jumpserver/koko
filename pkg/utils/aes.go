package utils

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"io"
)

func Encrypt(src, cipherKey string) (dst string, err error) {
	var encryptResult []byte
	encryptResult, err = aseGcmEncrypt([]byte(src), cipherKey)
	if err != nil {
		return
	}
	dst = encodeBase64(encryptResult)
	return
}

func encodeBase64(src []byte) string {
	return base64.StdEncoding.EncodeToString(src)
}

func aseGcmEncrypt(plainText []byte, cipherKey string) (result []byte, err error) {
	var (
		aesBlock cipher.Block
		gcm      cipher.AEAD
	)
	aesBlock, err = aes.NewCipher([]byte(cipherKey))
	if err != nil {
		return
	}
	gcm, err = cipher.NewGCM(aesBlock)
	if err != nil {
		return
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return
	}
	result = gcm.Seal(nonce, nonce, plainText, nil)
	return
}
