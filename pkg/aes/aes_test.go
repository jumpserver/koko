package aes

import (
	"testing"
)

func TestDecrypt(t *testing.T) {
	//var aesKey = "JumpServer Cipher Key for KoKo !" // create aes secret on build time
	text := "JumpServer Token Value"
	t.Log("Encrypt Text: ", text)
	dst, err := Encrypt(text)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Encrypt '%s' to '%s'", text, dst)
	decryptResult, err := Decrypt(dst)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Decrypt '%s' to '%s'", dst, decryptResult)
	if decryptResult != text {
		t.Fatalf("Decrypt %s error: %s\n", text, decryptResult)
	}

}
