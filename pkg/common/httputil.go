package common

import (
	"crypto/md5"
	"encoding/base64"
	"fmt"
	log "github.com/sirupsen/logrus"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func HTTPGMTDate() string {
	GmtDateLayout := "Mon, 02 Jan 2006 15:04:05 GMT"
	return time.Now().UTC().Format(GmtDateLayout)
}

func MakeSignature(key, date string) string {
	s := strings.Join([]string{key, date}, "\n")
	return Base64Encode(MD5Encode([]byte(s)))
}

func Base64Encode(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

func MD5Encode(b []byte) string {
	return fmt.Sprintf("%x", md5.Sum(b))
}

func MakeSureDirExit(filePath string) {
	dirPath := filepath.Dir(filePath)
	if _, err := os.Stat(dirPath); os.IsNotExist(err) {
		err = os.MkdirAll(dirPath, os.ModePerm)
		if err != nil {
			log.Info("could not create dir path:", dirPath)
			os.Exit(1)
		}
		log.Info("create dir path:", dirPath)
		return
	}
	log.Info("dir path exits:", dirPath)

}

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
