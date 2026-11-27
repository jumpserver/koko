package nosql

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const maxClusterMasters = 128

type clusterMaster struct {
	addr, id, slots string
}

type clusterScanState struct {
	node     int
	cursor   uint64
	topology string
	expires  time.Time
}

func (s *redisSession) clusterMasters(ctx context.Context) ([]clusterMaster, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	slots, err := s.cluster.ClusterSlots(ctx).Result()
	if err != nil {
		return nil, err
	}
	return redisClusterMasters(slots)
}

func redisClusterMasters(slots []redis.ClusterSlot) ([]clusterMaster, error) {
	byAddress := make(map[string]clusterMaster)
	var assigned [16384]bool
	sort.Slice(slots, func(i, j int) bool { return slots[i].Start < slots[j].Start })
	for _, slot := range slots {
		if len(slot.Nodes) == 0 || slot.Start < 0 || slot.End > 16383 || slot.Start > slot.End {
			return nil, errors.New("invalid Redis Cluster topology")
		}
		node := slot.Nodes[0]
		if node.Addr == "" {
			return nil, errors.New("Redis Cluster master has no address")
		}
		for index := slot.Start; index <= slot.End; index++ {
			if assigned[index] {
				return nil, errors.New("overlapping Redis Cluster slots")
			}
			assigned[index] = true
		}
		master := byAddress[node.Addr]
		if master.addr != "" && master.id != node.ID {
			return nil, errors.New("conflicting Redis Cluster master identities")
		}
		master.addr, master.id = node.Addr, node.ID
		master.slots += fmt.Sprintf("%d-%d,", slot.Start, slot.End)
		byAddress[node.Addr] = master
		if len(byAddress) > maxClusterMasters {
			return nil, errors.New("Redis Cluster exceeds 128 master nodes")
		}
	}
	for _, present := range assigned {
		if !present {
			return nil, errors.New("Redis Cluster has unassigned slots")
		}
	}
	masters := make([]clusterMaster, 0, len(byAddress))
	for _, master := range byAddress {
		masters = append(masters, master)
	}
	sort.Slice(masters, func(i, j int) bool { return masters[i].addr < masters[j].addr })
	if len(masters) == 0 {
		return nil, errors.New("Redis Cluster has no masters")
	}
	return masters, nil
}

// MONITOR cannot silently remain attached to a former set of masters. Use a
// separate read-only discovery connection, so cancellation leaves the command
// session and its cluster pool usable.
func (s *redisSession) watchMonitor(ctx context.Context, masters []clusterMaster, report func(error)) {
	client := s.clusterNode(masters[0])
	defer client.Close()
	stop := context.AfterFunc(ctx, func() { _ = client.Close() })
	defer stop()
	signature := clusterTopology(masters)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			check, cancel := context.WithTimeout(ctx, 5*time.Second)
			slots, err := client.ClusterSlots(check).Result()
			cancel()
			if err == nil {
				var current []clusterMaster
				current, err = redisClusterMasters(slots)
				if err == nil && clusterTopology(current) != signature {
					err = errors.New("Redis Cluster topology changed; restart MONITOR to include every master")
				}
			}
			if err != nil {
				report(err)
				return
			}
		}
	}
}

func (s *redisSession) clusterNode(master clusterMaster) *redis.Client {
	options := *s.options
	options.Addr = master.addr
	client := redis.NewClient(&options)
	client.AddHook(noRedisRetry{})
	return client
}

func clusterTopology(masters []clusterMaster) string {
	signature := sha256.New()
	for _, master := range masters {
		fmt.Fprintf(signature, "%s/%s/%s;", master.id, master.addr, master.slots)
	}
	return string(signature.Sum(nil))
}

func (s *redisSession) clusterExecute(ctx context.Context, command string, args []any) (any, error) {
	switch command {
	case "DBSIZE":
		if len(args) == 1 {
			return s.cluster.DBSize(ctx).Result()
		}
	case "SCAN":
		return s.clusterScan(ctx, args)
	case "KEYS":
		if len(args) == 2 {
			return s.clusterKeys(ctx, args[1].(string))
		}
	case "INFO":
		aggregate := len(args) == 1
		for _, section := range args[1:] {
			switch strings.ToLower(section.(string)) {
			case "keyspace", "default", "all", "everything":
				aggregate = true
			}
		}
		if aggregate {
			base, err := s.cluster.Do(ctx, args...).Text()
			if err != nil {
				return nil, err
			}
			keyspace, err := s.clusterKeyspace(ctx)
			if err != nil {
				return nil, err
			}
			return replaceRedisKeyspace(base, keyspace.(string)), nil
		}
	case `\CLUSTER`:
		if len(args) == 2 && strings.EqualFold(args[1].(string), "info") {
			return s.clusterInfo(ctx)
		}
		return nil, errors.New(`usage: \cluster info`)
	}
	return s.cluster.Do(ctx, args...).Result()
}

func (s *redisSession) clusterInfo(ctx context.Context) (any, error) {
	masters, err := s.clusterMasters(ctx)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var output strings.Builder
	var total int64
	fmt.Fprintln(&output, "NODE\tSLOTS\tKEYS")
	for _, master := range masters {
		client := s.clusterNode(master)
		n, err := client.DBSize(ctx).Result()
		_ = client.Close()
		if err != nil {
			return nil, fmt.Errorf("cluster statistics incomplete, node %s: %w", master.addr, err)
		}
		if n < 0 || n > math.MaxInt64-total {
			return nil, errors.New("invalid cluster DBSIZE counter")
		}
		total += n
		fmt.Fprintf(&output, "%s\t%s\t%d\n", master.addr, strings.TrimSuffix(master.slots, ","), n)
	}
	fmt.Fprintf(&output, "TOTAL\t%d masters\t%d", len(masters), total)
	return output.String(), nil
}

func (s *redisSession) clusterKeyspace(ctx context.Context) (any, error) {
	masters, err := s.clusterMasters(ctx)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var keys, expires int64
	weighted := new(big.Int)
	var size int
	for _, master := range masters {
		client := s.clusterNode(master)
		info, err := client.Info(ctx, "keyspace").Result()
		_ = client.Close()
		if err != nil {
			return nil, fmt.Errorf("cluster keyspace incomplete, node %s: %w", master.addr, err)
		}
		if len(info) > (8<<20)-size {
			return nil, errors.New("cluster keyspace exceeds 8 MiB")
		}
		size += len(info)
		for _, line := range strings.Split(info, "\n") {
			if !strings.HasPrefix(line, "db0:") {
				continue
			}
			var count, exp, avg int64
			for _, field := range strings.Split(strings.TrimSpace(strings.TrimPrefix(line, "db0:")), ",") {
				name, value, _ := strings.Cut(field, "=")
				if name != "keys" && name != "expires" && name != "avg_ttl" {
					continue
				}
				n, err := strconv.ParseInt(value, 10, 64)
				if err != nil || n < 0 {
					return nil, errors.New("invalid cluster keyspace counters")
				}
				switch name {
				case "keys":
					count = n
				case "expires":
					exp = n
				case "avg_ttl":
					avg = n
				}
			}
			if exp > count || count > math.MaxInt64-keys || exp > math.MaxInt64-expires {
				return nil, errors.New("cluster keyspace counter overflow")
			}
			keys += count
			expires += exp
			weighted.Add(weighted, new(big.Int).Mul(big.NewInt(avg), big.NewInt(exp)))
		}
	}
	var ttl int64
	if expires > 0 {
		ttl = weighted.Quo(weighted, big.NewInt(expires)).Int64()
	}
	if keys == 0 {
		return "# Keyspace\r\n", nil
	}
	return fmt.Sprintf("# Keyspace\r\ndb0:keys=%d,expires=%d,avg_ttl=%d\r\n", keys, expires, ttl), nil
}

func (s *redisSession) clusterScan(ctx context.Context, args []any) (any, error) {
	if len(args) < 2 {
		return nil, errors.New("SCAN requires a cursor")
	}
	cursor, err := strconv.ParseUint(args[1].(string), 10, 64)
	if err != nil {
		return nil, errors.New("invalid SCAN cursor")
	}
	for i := 2; i < len(args); i += 2 {
		if i+1 == len(args) {
			return nil, errors.New("invalid SCAN options")
		}
		switch strings.ToUpper(args[i].(string)) {
		case "MATCH", "TYPE":
		case "COUNT":
			count, err := strconv.ParseInt(args[i+1].(string), 10, 64)
			if err != nil || count < 1 || count > 10000 {
				return nil, errors.New("SCAN COUNT must be between 1 and 10000")
			}
		default:
			return nil, errors.New("invalid SCAN option")
		}
	}
	masters, err := s.clusterMasters(ctx)
	if err != nil {
		return nil, err
	}
	state := clusterScanState{topology: clusterTopology(masters)}
	if cursor != 0 {
		var found bool
		state, found = s.scans[cursor]
		if !found || time.Now().After(state.expires) {
			return nil, errors.New("SCAN cursor is unknown or expired; restart with SCAN 0")
		}
		if state.topology != clusterTopology(masters) {
			return nil, errors.New("cluster topology changed; restart with SCAN 0")
		}
	}
	if state.node >= len(masters) {
		return nil, errors.New("invalid cluster SCAN state")
	}
	client := s.clusterNode(masters[state.node])
	defer client.Close()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	keys, next, err := redisNodeScan(ctx, client, state.cursor, args[2:]...)
	if err != nil {
		return nil, err
	}
	state.cursor = next
	if next == 0 {
		state.node++
	}
	if state.node == len(masters) {
		return []any{"0", keys}, nil
	}
	if s.scans == nil {
		s.scans = make(map[uint64]clusterScanState)
	}
	var oldestID uint64
	var oldest time.Time
	for id, entry := range s.scans {
		if time.Now().After(entry.expires) {
			delete(s.scans, id)
			continue
		}
		if oldest.IsZero() || entry.expires.Before(oldest) {
			oldest, oldestID = entry.expires, id
		}
	}
	if len(s.scans) >= 1024 {
		delete(s.scans, oldestID)
	}
	var token [8]byte
	for {
		if _, err = rand.Read(token[:]); err != nil {
			return nil, err
		}
		next = binary.BigEndian.Uint64(token[:]) & ((1 << 52) - 1)
		if _, exists := s.scans[next]; next != 0 && !exists {
			break
		}
	}
	state.expires = time.Now().Add(5 * time.Minute)
	s.scans[next] = state
	return []any{strconv.FormatUint(next, 10), keys}, nil
}

// The SDK's Scan helper omits MATCH when its value is empty. Explicit MATCH
// "" must still be sent: dropping it would unexpectedly return every key.
func redisNodeScan(ctx context.Context, client *redis.Client, cursor uint64, options ...any) ([]string, uint64, error) {
	args := append([]any{"SCAN", strconv.FormatUint(cursor, 10)}, options...)
	cmd := redis.NewScanCmd(ctx, client.Process, args...)
	if err := client.Process(ctx, cmd); err != nil {
		return nil, 0, err
	}
	return cmd.Result()
}

func replaceRedisKeyspace(info, keyspace string) string {
	var output strings.Builder
	skip, found := false, false
	for _, line := range strings.Split(strings.ReplaceAll(info, "\r\n", "\n"), "\n") {
		if strings.HasPrefix(line, "# ") {
			skip = line == "# Keyspace"
			if skip {
				output.WriteString(keyspace)
				found = true
				continue
			}
		}
		if !skip && line != "" {
			output.WriteString(line)
			output.WriteString("\r\n")
		}
	}
	if !found {
		output.WriteString(keyspace)
	}
	return output.String()
}

func (s *redisSession) clusterKeys(ctx context.Context, pattern string) (any, error) {
	masters, err := s.clusterMasters(ctx)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	seen := make(map[string]struct{})
	keys := make([]string, 0)
	bytes := 0
	for _, master := range masters {
		client := s.clusterNode(master)
		var cursor uint64
		for pages := 0; ; pages++ {
			batch, next, err := redisNodeScan(ctx, client, cursor, "MATCH", pattern, "COUNT", 1000)
			if err != nil {
				_ = client.Close()
				return nil, err
			}
			for _, key := range batch {
				if _, exists := seen[key]; exists {
					continue
				}
				bytes += len(key)
				if len(keys) >= 10000 || bytes > 8<<20 {
					_ = client.Close()
					return nil, errors.New("KEYS exceeds cluster result limit; use SCAN")
				}
				seen[key] = struct{}{}
				keys = append(keys, key)
			}
			cursor = next
			if cursor == 0 {
				break
			}
			if pages >= 10000 {
				_ = client.Close()
				return nil, errors.New("KEYS scan limit exceeded; use SCAN")
			}
		}
		_ = client.Close()
	}
	return keys, nil
}
