package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/jumpserver/koko/internal/agentauth"
)

const (
	maxMessageMetadataBytes = 64 * 1024
	maxMessageMetadataDepth = 8
	maxMessageMetadataNodes = 2048
)

var sensitiveMetadataKeyParts = []string{
	"authorization", "authentication", "cookie", "password", "passwd",
	"passphrase", "secret", "token", "signature", "credential", "privatekey",
	"apikey", "accesskey", "hostkey", "csrf", "bearer",
}

func sanitizeMessageMetadata(source map[string]any) (map[string]any, error) {
	if source == nil {
		return nil, nil
	}
	nodes := 0
	if err := validateMetadataValue(source, 1, &nodes); err != nil {
		return nil, err
	}
	canonical, err := agentauth.CanonicalJSON(source)
	if err != nil || len(canonical) > maxMessageMetadataBytes {
		return nil, fmt.Errorf("message metadata is malformed or too large")
	}
	decoder := json.NewDecoder(bytes.NewReader(canonical))
	decoder.UseNumber()
	var normalized map[string]any
	if err = decoder.Decode(&normalized); err != nil {
		return nil, fmt.Errorf("message metadata is malformed")
	}
	return scrubMetadataMap(normalized), nil
}

func messageMetadataSize(value map[string]any) int {
	if value == nil {
		return 0
	}
	canonical, err := agentauth.CanonicalJSON(value)
	if err != nil {
		return maxMessageMetadataBytes
	}
	return len(canonical)
}

func validateMetadataValue(value any, depth int, nodes *int) error {
	if depth > maxMessageMetadataDepth {
		return fmt.Errorf("message metadata nesting is too deep")
	}
	*nodes++
	if *nodes > maxMessageMetadataNodes {
		return fmt.Errorf("message metadata has too many values")
	}
	switch item := value.(type) {
	case map[string]any:
		for key, child := range item {
			if !utf8.ValidString(key) {
				return fmt.Errorf("message metadata contains invalid text")
			}
			*nodes++
			if *nodes > maxMessageMetadataNodes {
				return fmt.Errorf("message metadata has too many values")
			}
			if err := validateMetadataValue(child, depth+1, nodes); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range item {
			if err := validateMetadataValue(child, depth+1, nodes); err != nil {
				return err
			}
		}
	case string:
		if !utf8.ValidString(item) {
			return fmt.Errorf("message metadata contains invalid text")
		}
	case nil, bool, float64, json.Number:
	default:
		return fmt.Errorf("message metadata contains a non-JSON value")
	}
	return nil
}

func scrubMetadataMap(source map[string]any) map[string]any {
	result := make(map[string]any, len(source))
	for key, value := range source {
		if sensitiveMetadataKey(key) {
			continue
		}
		result[key] = scrubMetadataValue(value)
	}
	return result
}

func scrubMetadataValue(value any) any {
	switch item := value.(type) {
	case map[string]any:
		return scrubMetadataMap(item)
	case []any:
		result := make([]any, len(item))
		for index := range item {
			result[index] = scrubMetadataValue(item[index])
		}
		return result
	default:
		return value
	}
}

func sensitiveMetadataKey(value string) bool {
	var normalized strings.Builder
	for _, char := range value {
		switch {
		case char >= 'A' && char <= 'Z':
			normalized.WriteRune(char + ('a' - 'A'))
		case char >= 'a' && char <= 'z', char >= '0' && char <= '9':
			normalized.WriteRune(char)
		}
	}
	key := normalized.String()
	for _, sensitive := range sensitiveMetadataKeyParts {
		if strings.Contains(key, sensitive) {
			return true
		}
	}
	return false
}
