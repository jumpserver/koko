package config

import "log"

func Initial(confPath string) {
	if err := Conf.Load(confPath); err != nil {
		log.Fatal(err)
	}
	log.Printf("Config file Path: %s\n", confPath)
	Conf.EnsureConfigValid()
}
