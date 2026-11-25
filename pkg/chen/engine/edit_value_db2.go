package engine

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// All Db2 date/time representations fit in 64 bytes after trimming whitespace.
// Check the input before the SQL cast, which could otherwise truncate it.
const db2TemporalBindSize = 64

var db2FractionalClock = regexp.MustCompile(`[0-9]{1,2}[:.][0-9]{2}[:.][0-9]{2}\.([0-9]+)`)

func db2EditTemporalType(baseType string) bool {
	return baseType == "DATE" || baseType == "TIME" || baseType == "TIMESTAMP"
}

func db2EditTextValue(dataType, baseType, text string) (any, error) {
	if db2EditTemporalType(baseType) {
		text = strings.TrimSpace(text)
		if len(text) > db2TemporalBindSize {
			return nil, errors.New("Enter a date or time within the supported length")
		}
		if baseType == "TIMESTAMP" {
			precision := 6 // Db2's default when the type has no explicit modifier.
			if _, modifier, specified := strings.Cut(dataType, "("); specified {
				if _, err := fmt.Sscanf(modifier, "%d)", &precision); err != nil || precision < 0 || precision > 12 {
					return nil, errors.New("The column's timestamp precision is unavailable")
				}
			}
			fraction := db2FractionalClock.FindStringSubmatch(text)
			if fraction != nil && len(strings.TrimRight(fraction[1], "0")) > precision {
				return nil, fmt.Errorf("The column supports at most %d fractional-second digits", precision)
			}
		}
		return text, nil
	}
	switch baseType {
	case "REAL", "FLOAT", "DOUBLE", "DOUBLE PRECISION":
		bits := 64
		if baseType == "REAL" {
			bits = 32
		}
		number, err := strconv.ParseFloat(text, bits)
		if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
			return nil, errors.New("Enter a finite number within the column's range")
		}
		return number, nil
	case "DECIMAL", "NUMERIC", "DEC":
		return db2EditDecimal(dataType, text)
	}
	return text, nil
}

// The driver's packed decimal encoder accepts plain digits and silently drops
// digits outside the parameter's precision and scale. Expand exponent notation
// only after verifying the result fits the catalog declaration.
func db2EditDecimal(dataType, text string) (string, error) {
	_, modifier, _ := strings.Cut(dataType, "(")
	var precision, scale int
	if _, err := fmt.Sscanf(modifier, "%d,%d)", &precision, &scale); err != nil || precision < 1 || precision > 31 || scale < 0 || scale > precision {
		return "", errors.New("The column's decimal precision and scale are unavailable")
	}
	parts := editIntegerParts.FindStringSubmatch(text)
	if parts == nil || parts[2] == "" && parts[3] == "" {
		return "", errors.New("Enter a valid number without separators")
	}
	digits := strings.TrimLeft(parts[2]+parts[3], "0")
	if digits == "" {
		return "0", nil
	}
	var exponent int64
	var err error
	if parts[4] != "" {
		exponent, err = strconv.ParseInt(parts[4], 10, 32)
	}
	point := int64(len(digits)-len(parts[3])) + exponent
	digits = strings.TrimRight(digits, "0")
	if err != nil || point > int64(precision-scale) || int64(len(digits))-point > int64(scale) {
		return "", fmt.Errorf("Enter a number that fits %s without losing digits", dataType)
	}
	switch {
	case point <= 0:
		digits = "0." + strings.Repeat("0", int(-point)) + digits
	case point >= int64(len(digits)):
		digits += strings.Repeat("0", int(point)-len(digits))
	default:
		digits = digits[:point] + "." + digits[point:]
	}
	if parts[1] == "-" {
		digits = "-" + digits
	}
	return digits, nil
}
