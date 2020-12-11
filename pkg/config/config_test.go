package config

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestConfig_Load(t *testing.T) {
	err := Conf.Load("./test_config.yml")
	if err != nil {
		t.Errorf("Load from yaml faild: %v", err)
	}
	data, _ := json.MarshalIndent(Conf, "", "    ")
	fmt.Println(string(data))
}
