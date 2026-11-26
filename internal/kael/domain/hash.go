package domain

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
)

func HashBytes(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func HashValue(value any) (string, error) {
	encoded, err := CanonicalJSON(value)
	if err != nil {
		return "", err
	}
	return HashBytes(encoded), nil
}

func CanonicalJSON(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var normalized any
	if err = decoder.Decode(&normalized); err != nil {
		return nil, err
	}
	var output bytes.Buffer
	if err = writeCanonical(&output, normalized); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func writeCanonical(output *bytes.Buffer, value any) error {
	switch item := value.(type) {
	case nil:
		output.WriteString("null")
	case bool:
		if item {
			output.WriteString("true")
		} else {
			output.WriteString("false")
		}
	case string:
		encoded, _ := json.Marshal(item)
		output.Write(encoded)
	case json.Number:
		if _, err := strconv.ParseFloat(item.String(), 64); err != nil {
			return fmt.Errorf("invalid JSON number")
		}
		output.WriteString(item.String())
	case []any:
		output.WriteByte('[')
		for index, child := range item {
			if index > 0 {
				output.WriteByte(',')
			}
			if err := writeCanonical(output, child); err != nil {
				return err
			}
		}
		output.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(item))
		for key := range item {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		output.WriteByte('{')
		for index, key := range keys {
			if index > 0 {
				output.WriteByte(',')
			}
			encoded, _ := json.Marshal(key)
			output.Write(encoded)
			output.WriteByte(':')
			if err := writeCanonical(output, item[key]); err != nil {
				return err
			}
		}
		output.WriteByte('}')
	default:
		return fmt.Errorf("unsupported JSON value %T", value)
	}
	return nil
}
