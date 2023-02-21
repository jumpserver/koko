package model

import (
	"encoding/json"
	"strings"

	"github.com/jumpserver/koko/pkg/jms-sdk-go/common"
)

var (
	_ json.Unmarshaler = (*LabelField)(nil)
	_ json.Marshaler   = (*LabelField)(nil)
)

type LabelField string

func (s LabelField) MarshalJSON() ([]byte, error) {
	return []byte(`"` + s + `"`), nil
}

func (s *LabelField) UnmarshalJSON(bytes []byte) error {
	var labelValue struct {
		Label string `json:"label"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(bytes, &labelValue); err != nil {
		return err
	}
	*s = LabelField(labelValue.Value)
	return nil
}

const (
	NORMALType  LabelField = "normal"
	TUNNELType  LabelField = "tunnel"
	COMMANDType LabelField = "command"
)

const (
	LoginFromSSH LabelField = "ST"
	LoginFromWeb LabelField = "WT"
)

type Session struct {
	ID         string         `json:"id,omitempty"`
	User       string         `json:"user"`
	Asset      string         `json:"asset"`
	Account    string         `json:"account"`
	LoginFrom  LabelField     `json:"login_from,omitempty"`
	RemoteAddr string         `json:"remote_addr"`
	Protocol   string         `json:"protocol"`
	DateStart  common.UTCTime `json:"date_start"`
	OrgID      string         `json:"org_id"`
	UserID     string         `json:"user_id"`
	AssetID    string         `json:"asset_id"`
	AccountID  string         `json:"account_id"`
	Type       LabelField     `json:"type"`
}

type ReplayVersion string

const (
	UnKnown  ReplayVersion = ""
	Version2 ReplayVersion = "2"
	Version3 ReplayVersion = "3"
)

const (
	SuffixReplayGz = ".replay.gz"
	SuffixCastGz   = ".cast.gz"
	SuffixCast     = ".cast"
	SuffixGz       = ".gz"
)

var SuffixMap = map[ReplayVersion]string{
	Version2: SuffixReplayGz,
	Version3: SuffixCastGz,
}

func ParseReplayVersion(gzFile string, defaultValue ReplayVersion) ReplayVersion {
	for version, suffix := range SuffixMap {
		if strings.HasSuffix(gzFile, suffix) {
			return version
		}
	}
	return defaultValue
}
