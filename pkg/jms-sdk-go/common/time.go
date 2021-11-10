package common

import (
	"errors"
	"fmt"
	"time"
)

const utcFormat = "2006-01-02 15:04:05 -0700"

func NewUTCTime(now time.Time) UTCTime {
	return UTCTime{now.UTC()}
}

func NewNowUTCTime() UTCTime {
	return UTCTime{time.Now().UTC()}
}

type UTCTime struct {
	time.Time
}

func (t UTCTime) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf(`"%s"`, t.Format(utcFormat))), nil
}

func (t *UTCTime) UnmarshalJSON(data []byte) (err error) {
	t.Time, err = parseTimeFromSupportedFormat(data)
	return err
}

var (
	supportedTimeFormat = []string{
		"2006/01/02 15:04:05 -0700",
		utcFormat,
		time.RFC3339,
	}
)

var (
	ErrUnSupportFormat = errors.New("unsupported time format")
)

func parseTimeFromSupportedFormat(data []byte) (time.Time, error) {
	for _, format := range supportedTimeFormat {
		if parseTime, err := time.Parse(fmt.Sprintf(
			`"%s"`, format), string(data)); err == nil {
			return parseTime, nil
		}
	}
	return time.Time{}, fmt.Errorf("%w: %s", ErrUnSupportFormat, data)
}
