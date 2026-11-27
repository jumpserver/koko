package main

import (
	"context"
	"log"
	"os"

	_ "github.com/jumpserver-dev/usql/drivers/dameng"
	_ "github.com/jumpserver-dev/usql/drivers/db2"
	_ "github.com/jumpserver-dev/usql/drivers/mysql"
	_ "github.com/jumpserver-dev/usql/drivers/oceanbase"
	_ "github.com/jumpserver-dev/usql/drivers/postgres"
	_ "github.com/jumpserver-dev/usql/store"
	_ "github.com/xo/usql/drivers/clickhouse"
	_ "github.com/xo/usql/drivers/oracle"
	_ "github.com/xo/usql/drivers/sqlserver"
)

func main() {
	if err := New(os.Args).ExecuteContext(context.Background()); err != nil {
		log.Fatal(err)
	}
}
