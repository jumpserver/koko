package config

import "log"

func Initial(confPath string) {
	if err := Conf.Load(confPath); err != nil {
		log.Fatal(err)
	}
	Conf.EnsureConfigValid()
}
