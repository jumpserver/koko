package config

func Initial() {
	configFile := "config.yml"
	conf = LoadFromYaml(configFile)
}
