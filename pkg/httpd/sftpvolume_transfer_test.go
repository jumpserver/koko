package httpd

import "testing"

func TestActiveTransferRejectsDifferentTransferID(t *testing.T) {
	volume := &sftpVolume{transferWrite: &cachedTransferFile{id: "first", path: "/stage"}}
	if _, _, _, err := volume.writeHandleLocked("second", "/stage", 0); err == nil {
		t.Fatal("expected an active transfer to reject a different transfer ID")
	}
}

func TestActiveTransferUsesContiguousCheckpoint(t *testing.T) {
	stagePath, err := transferStagePath("/target", "transfer")
	if err != nil {
		t.Fatal(err)
	}
	volume := &sftpVolume{transferWrite: &cachedTransferFile{id: "transfer", path: stagePath, committed: 2}}

	result, err := volume.transferStatus("transfer", "/target", 3)
	if err != nil {
		t.Fatal(err)
	}
	if result.CommittedBytes != 2 || result.State != "ready" {
		t.Fatalf("expected contiguous checkpoint, got %#v", result)
	}
}

func TestCompletedTransferIsIdempotent(t *testing.T) {
	volume := &sftpVolume{}
	result := sftpTransferResult{
		TransferID: "transfer", Path: "/target", TotalBytes: 3,
		CommittedBytes: 3, State: "completed",
	}
	volume.rememberCompletedTransferLocked(result, "checksum")

	got, ok := volume.completedTransferLocked("transfer", "/target", 3, "CHECKSUM")
	if !ok || got != result {
		t.Fatalf("expected completed transfer result, got %#v, %v", got, ok)
	}
	if _, ok := volume.completedTransferLocked("transfer", "/target", 3, "different"); ok {
		t.Fatal("accepted a completed transfer with a different checksum")
	}
}
