package srvconn

import (
	"fmt"
	"io/fs"
	"os"
	"testing"
	"testing/fstest"
)

func TestAgentDirectoryPagesContinueBeyondTwoHundred(t *testing.T) {
	files := fstest.MapFS{".hidden": {}}
	for i := 0; i < 450; i++ {
		files[fmt.Sprintf("file-%03d", i)] = &fstest.MapFile{}
	}
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		t.Fatal(err)
	}
	values := make([]os.FileInfo, len(entries))
	for i, entry := range entries {
		values[i], err = entry.Info()
		if err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]bool{}
	for offset := 0; offset < 450; offset += 200 {
		page := directoryPage(values, offset, 200, false, false)
		if len(page.Entries) != min(200, 450-offset) || page.HasMore != (offset < 400) || page.Limited {
			t.Fatalf("incorrect directory page at %d: %#v", offset, page)
		}
		for _, entry := range page.Entries {
			if seen[entry.Name()] || entry.Name() == ".hidden" {
				t.Fatal("duplicate or hidden directory entry")
			}
			seen[entry.Name()] = true
		}
	}
	if page := directoryPage(values[:20], 19, 200, false, true); !page.Limited || page.HasMore {
		t.Fatal("wire scan boundary disappeared")
	}
}
