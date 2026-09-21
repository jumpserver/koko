package srvconn

import (
	"net/url"
	"testing"
)

func TestDamengUSQLCommandArgs(t *testing.T) {
	option := &sqlOption{
		Schema:   "dameng",
		Host:     "127.0.0.1",
		Port:     5236,
		Username: "user@name",
		Password: "p@ss/word",
		DBName:   "APP",
	}

	args, err := option.USQLCommandArgs()
	if err != nil {
		t.Fatal(err)
	}
	if len(args) != 2 {
		t.Fatalf("unexpected argument count: %d", len(args))
	}
	dsn, err := url.Parse(args[0])
	if err != nil {
		t.Fatal(err)
	}
	password, _ := dsn.User.Password()
	if dsn.Scheme != "dameng" || dsn.Host != "127.0.0.1:5236" ||
		dsn.User.Username() != "user@name" || password != "p@ss/word" || dsn.Path != "/APP" {
		t.Fatalf("unexpected Dameng DSN: %s", args[0])
	}
	if args[1] != "--variable=PROMPT1=dameng%R%#" {
		t.Fatalf("unexpected Dameng prompt: %s", args[1])
	}
}
