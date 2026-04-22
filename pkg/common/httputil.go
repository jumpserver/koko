package common

import (
	"strconv"
	"strings"
)

func ConvertSizeToBytes(size string) int {
	defaultSize := 1024 * 1024 * 1024
	suffixes := []string{"M", "m", "g", "G"}
	for i := 0; i < len(suffixes); i++ {
		if strings.HasSuffix(size, suffixes[i]) {
			num := strings.TrimSuffix(size, suffixes[i])
			switch strings.ToLower(suffixes[i]) {
			case "m":
				if sizeNum, err := strconv.Atoi(num); err == nil {
					return sizeNum * 1024 * 1024
				}
			case "g":
				if sizeNum, err := strconv.Atoi(num); err == nil {
					return sizeNum * 1024 * 1024 * 1024
				}
			}
			break
		}
	}
	if sizeNum, err := strconv.Atoi(size); err == nil && sizeNum > 0 {
		return sizeNum
	}

	return defaultSize
}
