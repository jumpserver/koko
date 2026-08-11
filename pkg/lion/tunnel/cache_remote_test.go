package tunnel

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRedisTLSConfigRejectsInvalidCA(t *testing.T) {
	caPath := filepath.Join(t.TempDir(), "invalid-ca.pem")
	if err := os.WriteFile(caPath, []byte("not a certificate"), 0o600); err != nil {
		t.Fatalf("write invalid CA: %s", err)
	}

	if _, err := getRedisTLSCfg(&Config{SSLCa: caPath}); err == nil {
		t.Fatal("expected invalid Redis CA to be rejected")
	}
}

func TestRedisCacheRejectsEmptySentinelAddresses(t *testing.T) {
	if _, err := NewGuaTunnelRedisCache(Config{SentinelsHost: "primary/ , "}); err == nil {
		t.Fatal("expected empty Redis Sentinel addresses to be rejected")
	}
}

func TestRedisCacheIntegration(t *testing.T) {
	address := os.Getenv("LION_TEST_REDIS_ADDR")
	if address == "" {
		t.Skip("set LION_TEST_REDIS_ADDR to run the Redis integration check")
	}

	cache, err := NewGuaTunnelRedisCache(Config{
		Addr:     address,
		Password: os.Getenv("LION_TEST_REDIS_PASSWORD"),
		DBIndex:  9,
	})
	if err != nil {
		t.Fatalf("create Lion Redis cache: %s", err)
	}
	if cache.rdb.Options().DB != 9 {
		t.Fatalf("Redis DB = %d, want 9", cache.rdb.Options().DB)
	}
	if err = cache.Close(); err != nil {
		t.Fatalf("close Lion Redis cache: %s", err)
	}
}
