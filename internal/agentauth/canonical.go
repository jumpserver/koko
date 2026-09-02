package agentauth

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
)

func HashValue(value any) (string, error) {
	canonical, err := CanonicalJSON(value)
	if err != nil {
		return "", err
	}
	return HashBytes(canonical), nil
}

func HashRawJSON(value json.RawMessage) (string, error) {
	if len(bytes.TrimSpace(value)) == 0 {
		return HashBytes(nil), nil
	}
	canonical, err := CanonicalJSON(value)
	if err != nil {
		return "", err
	}
	return HashBytes(canonical), nil
}

func HashBytes(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

// CanonicalJSON uses sorted object keys, normalized numbers and no insignificant
// whitespace. It rejects trailing JSON instead of hashing an ambiguous prefix.
func CanonicalJSON(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var decoded any
	if err = decoder.Decode(&decoded); err != nil {
		return nil, err
	}
	var trailing any
	if err = decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("canonical JSON has trailing data")
	}
	var output bytes.Buffer
	if err = writeCanonical(&output, decoded); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func writeCanonical(output *bytes.Buffer, value any) error {
	switch item := value.(type) {
	case nil:
		output.WriteString("null")
	case bool:
		output.WriteString(strconv.FormatBool(item))
	case string:
		encoded, _ := json.Marshal(item)
		output.Write(encoded)
	case json.Number:
		normalized, err := normalizeNumber(item.String())
		if err != nil {
			return err
		}
		output.WriteString(normalized)
	case []any:
		output.WriteByte('[')
		for index := range item {
			if index > 0 {
				output.WriteByte(',')
			}
			if err := writeCanonical(output, item[index]); err != nil {
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
		return fmt.Errorf("unsupported canonical JSON value %T", value)
	}
	return nil
}

func normalizeNumber(value string) (string, error) {
	number, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return "", fmt.Errorf("invalid JSON number: %w", err)
	}
	if math.IsInf(number, 0) || math.IsNaN(number) {
		return "", fmt.Errorf("invalid non-finite JSON number")
	}
	if number == 0 {
		return "0", nil
	}
	if math.Trunc(number) == number && math.Abs(number) > 9007199254740991 {
		return "", fmt.Errorf("JSON integer exceeds the JavaScript-safe range")
	}
	absolute := math.Abs(number)
	if absolute >= 1e-6 && absolute < 1e21 {
		return strconv.FormatFloat(number, 'f', -1, 64), nil
	}
	formatted := strconv.FormatFloat(number, 'e', -1, 64)
	marker := strings.LastIndexByte(formatted, 'e')
	if marker < 0 {
		return formatted, nil
	}
	exponent, err := strconv.Atoi(formatted[marker+1:])
	if err != nil {
		return "", fmt.Errorf("normalize JSON exponent: %w", err)
	}
	sign := "+"
	if exponent < 0 {
		sign = "-"
		exponent = -exponent
	}
	return formatted[:marker] + "e" + sign + strconv.Itoa(exponent), nil
}
