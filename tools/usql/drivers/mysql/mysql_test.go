package mysql

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/xo/dburl"
	"github.com/xo/usql/drivers"
)

func TestMySQLConnectsToConfiguredAddress(t *testing.T) {
	for _, test := range []struct {
		name, host, query string
	}{
		{"IPv4", "127.0.0.1", ""},
		{"IPv6", "::1", ""},
		{"IPv6CustomTLS", "::1", "&tls=custom"},
	} {
		t.Run(test.name, func(t *testing.T) {
			listener, err := net.Listen("tcp", net.JoinHostPort(test.host, "0"))
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			if err := listener.(*net.TCPListener).SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
				t.Fatal(err)
			}
			served := make(chan error, 1)
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					served <- err
					return
				}
				defer conn.Close()
				// A valid MySQL error greeting proves that the driver reached this
				// exact TCP listener without requiring database authentication.
				payload := append([]byte{0xff, 0x15, 0x04, '#', 'H', 'Y', '0', '0', '0'}, []byte("address reached")...)
				packet := append([]byte{byte(len(payload)), 0, 0, 0}, payload...)
				_, err = conn.Write(packet)
				served <- err
			}()
			parsed, err := dburl.Parse("mysql://probe:test@" + listener.Addr().String() + "/?timeout=1s&readTimeout=1s" + test.query)
			if err != nil {
				t.Fatal(err)
			}
			discard := func() io.Writer { return io.Discard }
			db, err := drivers.Open(context.Background(), parsed, discard, discard)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			err = db.PingContext(ctx)
			var response *mysqldriver.MySQLError
			if !errors.As(err, &response) || response.Number != 1045 || response.Message != "address reached" {
				t.Fatalf("did not reach configured MySQL listener: %v", err)
			}
			if err := <-served; err != nil {
				t.Fatal(err)
			}
		})
	}
}
