package main

import (
	"context"
	"log"
	"os"

	_ "github.com/jumpserver/koko/tools/usql/drivers/dameng"
	_ "github.com/jumpserver/koko/tools/usql/drivers/db2"
	_ "github.com/jumpserver/koko/tools/usql/drivers/mysql"
	_ "github.com/jumpserver/koko/tools/usql/drivers/oceanbase"
	_ "github.com/jumpserver/koko/tools/usql/drivers/postgres"
	_ "github.com/xo/usql/drivers/clickhouse"
	_ "github.com/xo/usql/drivers/oracle"
	_ "github.com/xo/usql/drivers/sqlserver"
)

func main() {
	if err := New(os.Args).ExecuteContext(context.Background()); err != nil {
		log.Fatal(err)
	}
}
