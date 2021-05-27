package common

import (
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
	t.Time, err = time.Parse(fmt.Sprintf(
		`"%s"`, utcFormat), string(data))
	return err
}
