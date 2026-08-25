package proxy

import (
	"bytes"
	"testing"
	"unicode/utf8"

	"github.com/LeeEirc/terminalparser"
)

func benchmarkLongLineOutput(b *testing.B, chunkSize int) {
	line := bytes.Repeat([]byte("2026-08-25 INFO 测试长日志字段=value "), 512)
	data := bytes.Repeat(append(line, '\r', '\n'), 50)
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		parser := &TerminalParser{
			Ps1sStr: "$ ",
			Screen:  terminalparser.NewScreen(24, 80),
			state:   OutputState,
			cmd:     "tail -f long.log",
		}
		for offset := 0; offset < len(data); {
			end := min(offset+chunkSize, len(data))
			for end > offset && !utf8.Valid(data[offset:end]) {
				end--
			}
			parser.Feed(data[offset:end])
			offset = end
		}
	}
}

func BenchmarkLongLineOutput1KiB(b *testing.B) {
	benchmarkLongLineOutput(b, 1024)
}

func BenchmarkLongLineOutput8KiB(b *testing.B) {
	benchmarkLongLineOutput(b, 8*1024)
}

func TestParseCommandOutputPlainLongLines(t *testing.T) {
	line := bytes.Repeat([]byte("2026-08-25 INFO 测试长日志字段=value "), 512)
	data := bytes.Repeat(append(bytes.Clone(line), '\r', '\n'), 50)

	if !isPlainCommandOutput(data) {
		t.Fatal("long line fixture should use the plain output path")
	}

	rows := parseCommandOutput(data)
	if len(rows) != 51 {
		t.Fatalf("parsed row count = %d, want 51 including the trailing empty row", len(rows))
	}
	for index := 0; index < 50; index++ {
		if !bytes.Equal([]byte(rows[index]), line) {
			t.Fatalf("row %d was changed", index)
		}
	}
}

func TestIsPlainCommandOutput(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want bool
	}{
		{name: "utf8 crlf", data: []byte("日志内容\r\nnext\tvalue\n"), want: true},
		{name: "ansi sgr", data: []byte("\x1b[31mred\x1b[0m\r\n"), want: true},
		{name: "ansi cursor", data: []byte("\x1b[2Cmove"), want: false},
		{name: "cursor return", data: []byte("progress 10%\rprogress 20%"), want: false},
		{name: "control byte", data: []byte{'o', 'k', 0x08}, want: false},
		{name: "invalid utf8", data: []byte{'o', 'k', 0xff}, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isPlainCommandOutput(test.data); got != test.want {
				t.Fatalf("isPlainCommandOutput() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestParseCommandOutputStripsSGR(t *testing.T) {
	rows := parseCommandOutput([]byte("before \x1b[01;31mmatch\x1b[m after\r\n"))
	if len(rows) != 2 || rows[0] != "before match after" {
		t.Fatalf("parseCommandOutput() = %q", rows)
	}
}
