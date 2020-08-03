package aes

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
)

var cipherKey = "JumpServer Cipher Key for KoKo !"

func Encrypt(src string) (dst string, err error) {
	var encryptResult []byte
	encryptResult, err = aseGcmEncrypt([]byte(src), cipherKey)
	if err != nil {
		return
	}
	dst = encodeBase64(encryptResult)
	return
}

func Decrypt(src string) (dst string, err error) {
	var (
		encryptResult []byte
		decryptResult []byte
	)
	encryptResult, err = decodeBase64(src)
	if err != nil {
		return
	}
	decryptResult, err = aseGcmDecrypt(encryptResult, cipherKey)
	if err != nil {
		return
	}
	dst = string(decryptResult)
	return
}

var ErrGcmSize = errors.New("cipher: incorrect size given to GCM")

func encodeBase64(src []byte) string {
	return base64.StdEncoding.EncodeToString(src)
}

func decodeBase64(src string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(src)
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

func aseGcmDecrypt(encryptText []byte, cipherKey string) (result []byte, err error) {
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
	nonceSize := gcm.NonceSize()
	if len(encryptText) < nonceSize {
		err = ErrGcmSize
		return
	}
	nonce, cipherText := encryptText[:nonceSize], encryptText[nonceSize:]
	return gcm.Open(nil, nonce, cipherText, nil)
}
