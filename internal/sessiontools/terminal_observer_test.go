package sessiontools

import (
	"strings"
	"testing"
)

func TestTerminalObserverRequiresReadySQLPrompt(t *testing.T) {
	for _, tc := range []struct{ name, input string }{
		{"empty", ""}, {"partial", "mysql=> SEL"}, {"continuation", "mysql-> "},
		{"wrapped", "mysql=> " + strings.Repeat("x", 80)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			observer, err := NewTerminalObserver(80, 24, "mysql")
			if err != nil {
				t.Fatal(err)
			}
			defer observer.Close()
			observer.Feed([]byte(tc.input))
			if _, err = observer.Begin("SELECT 1;"); err == nil {
				t.Fatal("PTY execution accepted an unfinished terminal input")
			}
			observer.Feed([]byte("\r\x1b[Kmysql=> "))
			if _, err = observer.Begin("SELECT 1;"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestTerminalObserverIgnoresPromptRedraw(t *testing.T) {
	observer, err := NewTerminalObserver(80, 24, "mysql")
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Close()
	observer.Feed([]byte("mysql=> "))
	for _, command := range []string{"SELECT 1;", "SELECT 1 /* " + strings.Repeat("x", 160) + " */;"} {
		result, err := observer.Begin(command)
		if err != nil {
			t.Fatal(err)
		}
		observer.Feed([]byte("\r\x1b[Kmy"))
		observer.Feed([]byte("sql=> "))
		observer.Feed([]byte("\r\nmysql=> "))
		select {
		case <-result:
			t.Fatal("a prompt redraw completed the command")
		default:
		}
		observer.Feed([]byte(command + "\r\n1\r\nmysql=> "))
		select {
		case got := <-result:
			if got.Command != command || got.Output != "1" {
				t.Fatalf("unexpected command result: %#v", got)
			}
		default:
			t.Fatal("completed SQL did not finish the call")
		}
	}
}

func TestTerminalObserverCompletesAfterEchoScrollsOut(t *testing.T) {
	rows := strings.Repeat("1\r\n", 10000) + "mysql=> "
	for _, tc := range []struct {
		name      string
		chunk     int
		highlight bool
	}{
		{"chunked", 1024, false}, {"single_write", 0, false},
		{"highlighted_chunked", 7, true}, {"highlighted_single_write", 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			observer, err := NewTerminalObserver(80, 24, "mysql")
			if err != nil {
				t.Fatal(err)
			}
			defer observer.Close()
			observer.Feed([]byte("mysql=> "))
			result, err := observer.Begin("SELECT id FROM products;")
			if err != nil {
				t.Fatal(err)
			}
			echo := "SELECT id FROM products;\r\n"
			if tc.highlight {
				echo = "\x1b[32mSELECT\x1b[0m id \x1b[32mFROM\x1b[0m products;\r\n"
			}
			response := []byte(echo + rows)
			for data := response; len(data) > 0; {
				n := len(data)
				if tc.chunk > 0 {
					n = min(n, tc.chunk)
				}
				observer.Feed(data[:n])
				data = data[n:]
			}
			select {
			case <-result:
			default:
				t.Fatal("completed SQL remained active after its echo scrolled out")
			}
		})
	}
}

func TestTerminalObserverSQLPromptAliases(t *testing.T) {
	for _, tc := range []struct{ protocol, prompt string }{
		{"mariadb", "maria=> "}, {"postgresql", "postgres=# "}, {"ssh", "$ "},
	} {
		t.Run(tc.protocol, func(t *testing.T) {
			observer, err := NewTerminalObserver(80, 24, tc.protocol)
			if err != nil {
				t.Fatal(err)
			}
			defer observer.Close()
			observer.Feed([]byte(tc.prompt))
			result, err := observer.Begin("SELECT 1;")
			if err != nil {
				t.Fatal(err)
			}
			observer.Feed([]byte("SELECT 1;\r\n1\r\n" + tc.prompt))
			select {
			case <-result:
			default:
				t.Fatal("the protocol prompt did not finish the call")
			}
		})
	}
}
