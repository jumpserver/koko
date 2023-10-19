package utils

import (
	"bytes"
	"crypto/aes"
	"encoding/base64"
	"testing"
)

func TestDecrypt(t *testing.T) {
	var cipherKey = "JumpServer Cipher Key for KoKo !"
	text := "JumpServer Token Value"
	t.Log("Encrypt Text: ", text)
	dst, err := Encrypt(text, cipherKey)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Encrypt '%s' to '%s'", text, dst)
	decryptResult, err := Decrypt(dst, cipherKey)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Decrypt '%s' to '%s'", dst, decryptResult)
	if decryptResult != text {
		t.Fatalf("Decrypt %s error: %s\n", text, decryptResult)
	}

}

func TestEncrypt(t *testing.T) {
	secret := "4bd477efa46d4acea8016af7b332589d"
	src := "abc"
	ret, err := encryptECB([]byte(src), []byte(secret))
	if err != nil {
		t.Fatal(err)
	}

	t.Log(base64.StdEncoding.EncodeToString(ret))

}

func encryptECB(plaintext []byte, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	if len(plaintext)%aes.BlockSize != 0 {
		padding := aes.BlockSize - len(plaintext)%aes.BlockSize
		plaintext = append(plaintext, bytes.Repeat([]byte{byte(0x00)}, padding)...)
	}

	ciphertext := make([]byte, len(plaintext))
	for i := 0; i < len(plaintext); i += aes.BlockSize {
		block.Encrypt(ciphertext[i:i+aes.BlockSize], plaintext[i:i+aes.BlockSize])
	}

	return ciphertext, nil
}
