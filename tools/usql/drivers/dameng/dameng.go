// Package dameng registers DM Database support for usql.
package dameng

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	dm "gitee.com/chunanyong/dm"
	"github.com/xo/dburl"
	"github.com/xo/usql/drivers"
	"github.com/xo/usql/drivers/metadata"
	orameta "github.com/xo/usql/drivers/metadata/oracle"
)

const invalidUsernameOrPassword = -2501

var (
	statementEndRE = regexp.MustCompile(`;?\s*$`)
	blockEndRE     = regexp.MustCompile(`(?i)\send\s*;\s*$`)
)

func init() {
	registerURLScheme()
	drivers.Register("dameng", drivers.Driver{
		AllowMultilineComments: true,
		LowerColumnNames:       true,
		UseColumnTypes:         true,
		Version: func(ctx context.Context, db drivers.DB) (string, error) {
			var version string
			if err := db.QueryRowContext(ctx, `SELECT BANNER FROM V$VERSION`).Scan(&version); err != nil {
				return "", err
			}
			return version, nil
		},
		User: func(ctx context.Context, db drivers.DB) (string, error) {
			var username string
			if err := db.QueryRowContext(ctx, `SELECT USER FROM DUAL`).Scan(&username); err != nil {
				return "", err
			}
			return username, nil
		},
		Err: func(err error) (string, string) {
			var databaseError *dm.DmError
			if errors.As(err, &databaseError) {
				return strconv.FormatInt(int64(databaseError.ErrCode), 10), databaseError.Error()
			}
			return "", err.Error()
		},
		IsPasswordErr: func(err error) bool {
			var databaseError *dm.DmError
			return errors.As(err, &databaseError) && databaseError.ErrCode == invalidUsernameOrPassword
		},
		Process: func(_ *dburl.URL, prefix string, sqlstr string) (string, string, bool, error) {
			if !blockEndRE.MatchString(sqlstr) {
				sqlstr = statementEndRE.ReplaceAllString(sqlstr, "")
			}
			typ, query := drivers.QueryExecType(prefix, sqlstr)
			return typ, sqlstr, query, nil
		},
		NewMetadataReader: orameta.NewReader(),
		NewMetadataWriter: func(db drivers.DB, writer io.Writer, opts ...metadata.ReaderOption) metadata.Writer {
			return metadata.NewDefaultWriter(orameta.NewReader()(db, opts...))(db, writer)
		},
		Copy: drivers.CopyWithInsert(func(index int) string {
			return fmt.Sprintf(":%d", index)
		}),
	})
}

func registerURLScheme() {
	generate := dburl.GenFromURL("dm://localhost:5236")
	dburl.Register(dburl.Scheme{
		Driver:    "dameng",
		Transport: dburl.TransportTCP,
		Aliases:   []string{"dm"},
		Generator: func(u *dburl.URL) (string, string, error) {
			dsn, _, err := generate(u)
			if err != nil || u.User == nil {
				return strings.TrimSpace(dsn), "dm", err
			}
			// The DM driver splits userinfo itself but does not URL-decode it.
			// Pass the decoded credentials to the driver; otherwise a password
			// containing '@' is sent as '%40' and authentication fails.
			username := u.User.Username()
			if strings.Contains(username, ":") {
				return "", "", errors.New("Dameng usernames containing ':' are not supported by the DM driver")
			}
			userinfo := username
			if password, ok := u.User.Password(); ok {
				userinfo += ":" + password
			}
			at := strings.Index(dsn, "@")
			if at < 0 {
				return "", "", errors.New("Dameng DSN is missing userinfo separator")
			}
			suffix := dsn[at:]
			hasQuery := strings.Contains(suffix[1:], "?")
			dsn = "dm://" + userinfo + suffix
			// The DM driver uses the last '?' as its query separator. When a
			// credential contains '?' without query options, give it a trailing
			// empty query so that the credential is not mistaken for the query.
			if strings.Contains(userinfo, "?") && !hasQuery {
				dsn += "?"
			}
			return strings.TrimSpace(dsn), "dm", nil
		},
	})
}
