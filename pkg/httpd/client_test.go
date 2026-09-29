package httpd

import (
	"context"
	"testing"
)

func TestInputLockPreservesActiveCall(t *testing.T) {
	first, cancelFirst := context.WithCancel(context.Background())
	defer cancelFirst()
	second, cancelSecond := context.WithCancel(context.Background())
	defer cancelSecond()
	client := &Client{}
	if !client.setInputLock(cancelFirst) || client.setInputLock(cancelSecond) {
		t.Fatal("concurrent call replaced the active input lock")
	}
	// Input stays protected, and Ctrl-C cancels the active call rather than the contender.
	client.WriteData([]byte("Get-Location\r"))
	client.WriteData([]byte{3})
	if first.Err() == nil || second.Err() != nil {
		t.Fatal("Ctrl-C cancelled the wrong call")
	}
	client.setInputLock(nil)
	if !client.setInputLock(cancelSecond) {
		t.Fatal("completed call did not release the input lock")
	}
	client.WriteData([]byte{3})
	if second.Err() == nil {
		t.Fatal("next call did not acquire cancellation ownership")
	}
}
