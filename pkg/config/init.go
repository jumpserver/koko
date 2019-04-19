package config

func Initial() {
	configFile := "config.yml"
	_ = Conf.Load(configFile)
	Conf.EnsureConfigValid()
}
