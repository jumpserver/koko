package common

import (
	"context"
	"fmt"
	"net"
	"strconv"

	"github.com/go-redis/redis/v8"
)

func RedisClusterSlots(
	addrs []string, options redis.Options,
) func(context.Context) ([]redis.ClusterSlot, error) {
	addresses := append([]string(nil), addrs...)
	return func(ctx context.Context) ([]redis.ClusterSlot, error) {
		var lastErr error
		for _, addr := range addresses {
			clientOptions := options
			clientOptions.Addr = addr
			client := redis.NewClient(&clientOptions)
			value, err := client.Do(ctx, "CLUSTER", "SLOTS").Result()
			_ = client.Close()
			if err == nil {
				var slots []redis.ClusterSlot
				slots, err = parseRedisClusterSlots(value)
				if err == nil {
					return slots, nil
				}
			}
			lastErr = err
		}
		if lastErr == nil {
			lastErr = fmt.Errorf("Redis cluster has no addresses")
		}
		return nil, lastErr
	}
}

func parseRedisClusterSlots(value interface{}) ([]redis.ClusterSlot, error) {
	rows, ok := value.([]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid Redis cluster slots response %T", value)
	}
	slots := make([]redis.ClusterSlot, 0, len(rows))
	for _, value := range rows {
		row, ok := value.([]interface{})
		if !ok || len(row) < 3 {
			return nil, fmt.Errorf("invalid Redis cluster slot %T", value)
		}
		start, startOK := row[0].(int64)
		end, endOK := row[1].(int64)
		if !startOK || !endOK {
			return nil, fmt.Errorf("invalid Redis cluster slot range")
		}
		slot := redis.ClusterSlot{Start: int(start), End: int(end)}
		for _, value := range row[2:] {
			node, ok := value.([]interface{})
			if !ok || len(node) < 2 {
				return nil, fmt.Errorf("invalid Redis cluster node %T", value)
			}
			host, hostOK := node[0].(string)
			port, portOK := node[1].(int64)
			if !hostOK || !portOK {
				return nil, fmt.Errorf("invalid Redis cluster node address")
			}
			clusterNode := redis.ClusterNode{
				Addr: net.JoinHostPort(host, strconv.FormatInt(port, 10)),
			}
			if len(node) >= 3 {
				clusterNode.ID, _ = node[2].(string)
			}
			slot.Nodes = append(slot.Nodes, clusterNode)
		}
		slots = append(slots, slot)
	}
	return slots, nil
}
