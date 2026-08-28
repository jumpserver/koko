package httpd

import (
	"encoding/json"
	"testing"
)

func TestSFTPTerminalConfigDecodesFileAISettings(t *testing.T) {
	payload := []byte(`{
		"FTP_FILE_MAX_STORE": 64,
		"CHAT_AI_TYPE": "gpt",
		"GPT_MODEL": "legacy-model",
		"CHAT_AI_ENABLED": true,
		"CHAT_AI_PROVIDER": "openai",
		"CHAT_AI_MODEL": "file-model"
	}`)
	var config sftpTerminalConfig
	if err := json.Unmarshal(payload, &config); err != nil {
		t.Fatal(err)
	}
	settings := config.fileAISettings()
	if config.MaxStoreFTPFileSize != 64 || settings.ChatAIType != "gpt" ||
		settings.GptModel != "legacy-model" || settings.Provider != "openai" ||
		settings.Model != "file-model" || settings.Enabled == nil || !*settings.Enabled {
		t.Fatalf("unexpected aggregate terminal config: %#v %#v", config, settings)
	}
}
