package utils

import (
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
