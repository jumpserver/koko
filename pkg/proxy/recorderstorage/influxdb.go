package recorderstorage

import (
	"encoding/json"
	"fmt"
	"github.com/influxdata/influxdb-client-go/v2"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/pkg/errors"
	"strconv"
)

type InfluxdbClientConf struct {
	ServerURL string
	AuthToken string
	Bucket    string
	Measurement string
}

func (ic InfluxdbClientConf) Validate() error {
	if ic.ServerURL == "" || ic.Bucket == "" || ic.Measurement == "" {
		errMsg := fmt.Sprintf("Get influxdb conf failed.")
		return errors.Errorf(errMsg)
	}
	return nil
}

type InfluxdbCommandStorage struct {
	InfluxdbClientConf
	Client    influxdb2.Client
}

func NewInfluxdbClient(serverURL, authToken string) influxdb2.Client {
   return influxdb2.NewClient(serverURL, authToken)
}

func (is InfluxdbCommandStorage) TypeName() string {
	return "influxdb"
}

func (is InfluxdbCommandStorage) BulkSave(commands []*model.Command) error {
	for _, item := range commands {
		tags := map[string]string{
			"user": item.User,
			"systemUser": item.SystemUser,
			"server": item.Server,
			"riskLevel": strconv.Itoa(int(item.RiskLevel)),
			"dateCreated": item.DateCreated.String(),
		}
		itemBytes, err := json.Marshal(item)
		if err != nil {
			errMsg := fmt.Sprintf("Marshal command err: %v", err.Error())
			logger.Error(errMsg)
			return errors.New(errMsg)
		}
		field := map[string]interface{}{
			"value": string(itemBytes),
		}
		writeApi := is.Client.WriteAPI("", is.Bucket)
		p := influxdb2.NewPoint(is.Measurement, tags, field, item.DateCreated)
		writeApi.WritePoint(p)
	}
	return nil
}

