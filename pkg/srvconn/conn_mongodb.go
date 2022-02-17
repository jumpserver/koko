package srvconn

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/jumpserver/koko/pkg/localcommand"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	mongodbPrompt = "Enter password:"
)

var (
	_ ServerConnection = (*MongoDBConn)(nil)
)

func NewMongoDBConnection(ops ...SqlOption) (*MongoDBConn, error) {
	var (
		lCmd *localcommand.LocalCommand
		err  error
	)
	args := &sqlOption{
		Username: os.Getenv("USER"),
		Password: os.Getenv("PASSWORD"),
		Host:     "127.0.0.1",
		Port:     27017,
		DBName:   "test",
		win: Windows{
			Width:  80,
			Height: 120,
		},
	}
	for _, setter := range ops {
		setter(args)
	}
	if err := checkMongoDBAccount(args); err != nil {
		return nil, err
	}
	lCmd, err = startMongoDBCommand(args)

	if err != nil {
		return nil, err
	}
	err = lCmd.SetWinSize(args.win.Width, args.win.Height)
	if err != nil {
		_ = lCmd.Close()
		return nil, err
	}
	return &MongoDBConn{options: args, LocalCommand: lCmd}, nil
}

type MongoDBConn struct {
	options *sqlOption
	*localcommand.LocalCommand
}

func (conn *MongoDBConn) KeepAlive() error {
	return nil
}

func (conn *MongoDBConn) Close() error {
	_, _ = conn.Write([]byte("\r\nexit\r\n"))
	return conn.LocalCommand.Close()
}

func startMongoDBCommand(opt *sqlOption) (lcmd *localcommand.LocalCommand, err error) {
	cmd := opt.MongoDBCommandArgs()
	lcmd, err = localcommand.New("mongosh", cmd, localcommand.WithPtyWin(opt.win.Width, opt.win.Height))
	if err != nil {
		return nil, err
	}
	// 清除掉连接信息
	prefix := fmt.Sprintf("mongosh mongodb://%s:%s/%s?directConnection=ture", opt.Host, strconv.Itoa(opt.Port), opt.DBName)
	prompt := make([]byte, len(prefix)+7)
	_, _ = lcmd.Read(prompt[:])
	if opt.Password != "" {
		lcmd, err = MatchLoginPrefix(mongodbPrompt, "MongoDB", lcmd)
		if err != nil {
			return lcmd, err
		}
		lcmd, err = DoLogin(opt, lcmd, "MongoDB")
		if err != nil {
			return lcmd, err
		}
	}
	return lcmd, nil
}

func (opt *sqlOption) MongoDBCommandArgs() []string {
	attr := fmt.Sprintf("mongodb://%s:%s/%s", opt.Host, strconv.Itoa(opt.Port), opt.DBName)
	params := []string{
		attr, "--username", opt.Username,
	}
	return params
}

func checkMongoDBAccount(args *sqlOption) error {
	addr := fmt.Sprintf("mongodb://%s:%s", args.Host, strconv.Itoa(args.Port))
	clientOptions := options.Client().ApplyURI(addr)
	client, err := mongo.Connect(context.TODO(), clientOptions)
	if err != nil {
		return err
	}
	defer client.Disconnect(context.TODO())

	err = client.Ping(context.TODO(), nil)
	if err != nil {
		return err
	}
	return nil
}
