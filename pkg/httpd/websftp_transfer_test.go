package httpd

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"runtime"
	"testing"

	"github.com/jumpserver/koko/pkg/httpd/ws"
)

type transferWriteTestFS struct{ sftpFileSystem }

func (transferWriteTestFS) CheckUploadSize(string, int64) error { return nil }

func TestWebSFTPTransferWriteOrder(t *testing.T) {
	previous := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previous)
	for _, transport := range []string{"json", "binary"} {
		t.Run(transport, func(t *testing.T) {
			file, err := os.CreateTemp(t.TempDir(), "upload")
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			const transferID, target = "ordered", "/upload"
			stage, err := transferStagePath(target, transferID)
			if err != nil {
				t.Fatal(err)
			}
			volume := newSFTPVolume(transferWriteTestFS{}, nil)
			volume.transferWrite = newCachedTransferFile(transferID, stage, file, 0)
			responses := make(chan *Message, 2)
			handler := newWebSFTP(&UserWebsocket{
				conn:           ws.NewSocket(nil, httptest.NewRequest("GET", "/", nil)),
				messageChannel: responses,
				done:           make(chan struct{}),
			})
			handler.volume = volume
			close(handler.ready)
			var messages [2]Message
			for index := range messages {
				data := []byte{byte('a' + index)}
				metadata, err := json.Marshal(webSftpRequest{
					TransferID: transferID, Path: target, Size: 2,
					OffSet: int64(index), SHA256: sha256Hex(data),
				})
				if err != nil {
					t.Fatal(err)
				}
				messages[index] = Message{Id: string(data), Type: SFTPData, Cmd: "transfer_write", Data: string(metadata), Raw: data}
				if transport == "binary" {
					frame, err := encodeSftpBinaryFrame(&messages[index])
					if err != nil {
						t.Fatal(err)
					}
					messages[index] = Message{Type: TerminalBinary, Raw: frame}
				}
			}
			// Queue adjacent chunks before workers run, allowing the later write
			// to finish first while admission must follow WebSocket receive order.
			for index := range messages {
				handler.HandleMessage(&messages[index])
			}
			handler.pending.Wait()
			for range messages {
				response := <-responses
				if response.Err != "" {
					t.Fatalf("ordered chunk %s failed: %s", response.Id, response.Err)
				}
			}
			var contents [2]byte
			if _, err := file.ReadAt(contents[:], 0); err != nil || string(contents[:]) != "ab" {
				t.Fatalf("uploaded contents = %q, err = %v", contents, err)
			}
			checksum, err := chainReaderAt(file, 2, 1)
			if err != nil || checksum != volume.transferWrite.chain.hex() {
				t.Fatalf("upload checksum differs from stored bytes: %v", err)
			}
		})
	}
}

func TestPrepareTransferWriteValidation(t *testing.T) {
	const transferID, target = "validation", "/upload"
	stage, err := transferStagePath(target, transferID)
	if err != nil {
		t.Fatal(err)
	}
	volume := newSFTPVolume(transferWriteTestFS{}, nil)
	volume.transferWrite = newCachedTransferFile(transferID, stage, nil, 0)
	data := []byte("a")
	for _, test := range []struct {
		offset   int64
		checksum string
	}{
		{offset: 0, checksum: sha256Hex([]byte("b"))},
		{offset: 1, checksum: sha256Hex(data)},
	} {
		if write, err := volume.prepareTransferWrite(transferID, target, 2, test.offset, test.checksum, data); err == nil || write != nil {
			t.Fatal("accepted a corrupt or out-of-order chunk")
		}
	}
}
