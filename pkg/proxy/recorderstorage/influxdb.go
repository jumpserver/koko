package recorderstorage

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/influxdata/influxdb-client-go/v2"

	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
	"github.com/jumpserver/koko/pkg/logger"
)

type InfluxdbStorage struct {
	ServerURL   string
	AuthToken   string
	Bucket      string
	Measurement string
}

func NewInfluxdbClient(serverURL, authToken string) influxdb2.Client {
	return influxdb2.NewClient(serverURL, authToken)
}

func (influx InfluxdbStorage) BulkSave(commands []*model.Command) (err error) {
	client := NewInfluxdbClient(influx.ServerURL, influx.AuthToken)
	defer client.Close()
	for _, item := range commands {
		tags := map[string]string{
			"session":     item.SessionID,
			"orgId":       item.OrgID,
			"input":       item.Input,
			"output":      item.Output,
			"account":     item.Account,
			"user":        item.User,
			"asset":       item.Server,
			"riskLevel":   strconv.Itoa(int(item.RiskLevel)),
			"timeStamp":   strconv.FormatInt(item.Timestamp, 10),
			"dateCreated": item.DateCreated.String(),
		}
		itemBytes, _ := json.Marshal(item)
		field := map[string]interface{}{
			"value": string(itemBytes),
		}
		writeApi := client.WriteAPIBlocking("", influx.Bucket)
		p := influxdb2.NewPoint(influx.Measurement, tags, field, item.DateCreated)
		if err1 := writeApi.WritePoint(context.Background(), p); err1 != nil {
			logger.Errorf("Influxdb write point err: %s", err1)
			return err1
		}
	}
	return
}

func (influx InfluxdbStorage) TypeName() string {
	return "influxdb"
}
