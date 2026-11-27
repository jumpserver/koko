package nosql

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestReplaceRedisKeyspace(t *testing.T) {
	info := "# Server\r\nredis_version:8\r\n# Keyspace\r\ndb0:keys=1,expires=0,avg_ttl=0\r\n# Other\r\nvalue:yes\r\n"
	got := replaceRedisKeyspace(info, "# Keyspace\r\ndb0:keys=300,expires=0,avg_ttl=0\r\n")
	if strings.Count(got, "# Keyspace") != 1 || !strings.Contains(got, "value:yes") || !strings.Contains(got, "keys=300,") || strings.Contains(got, "keys=1,") {
		t.Fatal(got)
	}
}

func TestLiveRedisCluster(t *testing.T) {
	u := liveURL(t, "USQL_REDIS_CLUSTER_TEST_DSN_FILE")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	backend, err := openRedis(ctx, u, false)
	if err != nil {
		t.Fatal(err)
	}
	s := backend.(*redisSession)
	defer s.Close()
	if s.cluster == nil {
		t.Fatal("test DSN must have cluster=true")
	}
	base, err := s.cluster.DBSize(ctx).Result()
	if err != nil {
		t.Fatal(err)
	}
	prefix := fmt.Sprintf("usql-cluster-%d-", time.Now().UnixNano())
	var keys []string
	defer func() {
		for _, key := range keys {
			_ = s.cluster.Del(context.Background(), key).Err()
		}
	}()
	for i := 0; i < 300; i++ {
		key := prefix + strconv.Itoa(i)
		if err := s.cluster.Set(ctx, key, "value", 0).Err(); err != nil {
			t.Fatal(err)
		}
		keys = append(keys, key)
	}
	var output bytes.Buffer
	if err := s.Execute(ctx, &output, "DBSIZE"); err != nil || strings.TrimSpace(output.String()) != strconv.FormatInt(base+300, 10) {
		t.Fatalf("DBSIZE=%q %v", output.String(), err)
	}
	result, err := s.clusterExecute(ctx, "KEYS", []any{"KEYS", prefix + "*"})
	if err != nil || len(result.([]string)) != 300 {
		t.Fatalf("KEYS=%v %v", result, err)
	}
	seen := make(map[string]bool)
	cursor := "0"
	for page := 0; ; page++ {
		if page >= 1000 {
			t.Fatal("SCAN never finished")
		}
		value, err := s.clusterScan(ctx, []any{"SCAN", cursor, "MATCH", prefix + "*", "COUNT", "5"})
		if err != nil {
			t.Fatal(err)
		}
		parts := value.([]any)
		for _, key := range parts[1].([]string) {
			seen[key] = true
		}
		cursor = parts[0].(string)
		if cursor == "0" {
			break
		}
	}
	if len(seen) != 300 {
		t.Fatalf("SCAN found %d keys", len(seen))
	}
	empty, err := s.clusterKeys(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range empty.([]string) {
		if key != "" {
			t.Fatalf("empty KEYS pattern returned %q", key)
		}
	}
	for _, command := range []string{"INFO keyspace", "INFO", `\cluster info`} {
		output.Reset()
		if err := s.Execute(ctx, &output, command); err != nil {
			t.Fatalf("%s: %v", command, err)
		}
		if !strings.Contains(output.String(), strconv.FormatInt(base+300, 10)) {
			t.Fatalf("%s did not aggregate masters: %s", command, output.String())
		}
	}
	// A single scan may have more than 1024 pages. The checkpoint bound must
	// evict old checkpoints, not terminate the current traversal.
	s.scans = make(map[uint64]clusterScanState)
	for id := uint64(1); id <= 1024; id++ {
		s.scans[id] = clusterScanState{expires: time.Now().Add(time.Minute)}
	}
	if _, err := s.clusterScan(ctx, []any{"SCAN", "0", "COUNT", "1"}); err != nil {
		t.Fatal(err)
	}
	if len(s.scans) > 1024 {
		t.Fatal("unbounded scan checkpoints")
	}
	masters, err := s.clusterMasters(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("validated %d masters, 300 keys (replicas excluded)", len(masters))
}

func TestLiveRedisClusterStreams(t *testing.T) {
	u := liveURL(t, "USQL_REDIS_CLUSTER_TEST_DSN_FILE")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	backend, err := openRedis(ctx, u, false)
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	s := backend.(*redisSession)
	masters, err := s.clusterMasters(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"SUBSCRIBE usql_cluster_stream", "PSUBSCRIBE usql_cluster_*", "SSUBSCRIBE usql_cluster_stream", "MONITOR"} {
		t.Run(command, func(t *testing.T) {
			streamCtx, stop := context.WithCancel(ctx)
			defer stop()
			reader, writer := io.Pipe()
			defer reader.Close()
			done := make(chan error, 1)
			go func() { err := s.Execute(streamCtx, writer, command); _ = writer.Close(); done <- err }()
			scanner := bufio.NewScanner(reader)
			if !scanner.Scan() {
				t.Fatal("stream did not start")
			}
			marker := fmt.Sprintf("usql-stream-%d", time.Now().UnixNano())
			wanted := make(map[string]bool)
			if command == "MONITOR" {
				for _, master := range masters {
					value := marker + master.addr
					client := s.clusterNode(master)
					err := client.Echo(ctx, value).Err()
					_ = client.Close()
					if err != nil {
						t.Fatal(err)
					}
					wanted[value] = true
				}
			} else {
				if strings.HasPrefix(command, "SSUBSCRIBE") {
					err = s.cluster.SPublish(ctx, "usql_cluster_stream", marker).Err()
				} else {
					err = s.cluster.Publish(ctx, "usql_cluster_stream", marker).Err()
				}
				if err != nil {
					t.Fatal(err)
				}
				wanted[marker] = true
			}
			for len(wanted) > 0 && scanner.Scan() {
				for value := range wanted {
					if strings.Contains(scanner.Text(), value) {
						delete(wanted, value)
					}
				}
			}
			if len(wanted) > 0 {
				t.Fatalf("stream missed %v", wanted)
			}
			stop()
			_ = reader.Close()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) && !errors.Is(err, io.ErrClosedPipe) {
					t.Fatal(err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("stream did not stop")
			}
			if err := s.Execute(ctx, io.Discard, "PING"); err != nil {
				t.Fatal("command session unusable after cancellation", err)
			}
		})
	}
}
