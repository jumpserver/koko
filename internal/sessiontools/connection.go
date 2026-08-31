package sessiontools

import (
	"context"
	"fmt"
	"strings"

	"github.com/jumpserver/koko/pkg/srvconn"
)

func NewDatabaseExecutor(
	ctx context.Context,
	config DatabaseConfig,
) (CommandExecutor, error) {
	config.Protocol = strings.ToLower(strings.TrimSpace(config.Protocol))
	switch config.Protocol {
	case srvconn.ProtocolMySQL, srvconn.ProtocolMariadb:
		return NewMySQLExecutor(ctx, config)
	case srvconn.ProtocolPostgresql, srvconn.ProtocolSQLServer,
		srvconn.ProtocolOracle, srvconn.ProtocolClickHouse:
		return NewNativeSQLExecutor(ctx, config)
	case srvconn.ProtocolRedis:
		return NewRedisExecutor(ctx, config)
	case srvconn.ProtocolMongoDB:
		return NewMongoDBExecutor(ctx, config)
	default:
		return nil, fmt.Errorf("unsupported database command protocol %q", config.Protocol)
	}
}

func ProtocolCommandValidator(protocol string) CommandValidator {
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	return func(command string) (CommandConstraints, error) {
		constraints := CommandConstraints{BackgroundEligible: true}
		switch protocol {
		case srvconn.ProtocolSSH:
			constraints.BackgroundEligible = !isInteractiveCommand(command)
		case srvconn.ProtocolMySQL, srvconn.ProtocolMariadb,
			srvconn.ProtocolPostgresql, srvconn.ProtocolSQLServer,
			srvconn.ProtocolOracle, srvconn.ProtocolClickHouse:
			analysis, err := analyzeSQL(command)
			if err != nil {
				return CommandConstraints{}, err
			}
			constraints.BackgroundEligible = analysis.BackgroundEligible()
		case srvconn.ProtocolRedis:
			arguments, err := parseRedisCommand(command)
			if err != nil {
				return CommandConstraints{}, err
			}
			constraints.BackgroundEligible = redisBackgroundEligible(arguments)
		case srvconn.ProtocolMongoDB:
			document, err := parseMongoDBCommand(command)
			if err != nil {
				return CommandConstraints{}, err
			}
			constraints.BackgroundEligible = mongoDBBackgroundEligible(document)
		default:
			// Protocols without a detached executor can still use the active,
			// audited PTY. They must never be selected for background execution.
			constraints.BackgroundEligible = false
		}
		return constraints, nil
	}
}

func ProtocolSupportsBackgroundExecutor(protocol string) bool {
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	switch protocol {
	case srvconn.ProtocolSSH, srvconn.ProtocolMySQL, srvconn.ProtocolMariadb,
		srvconn.ProtocolPostgresql, srvconn.ProtocolSQLServer,
		srvconn.ProtocolOracle, srvconn.ProtocolClickHouse,
		srvconn.ProtocolRedis, srvconn.ProtocolMongoDB:
		return true
	default:
		return false
	}
}

func isInteractiveCommand(command string) bool {
	lower := strings.ToLower(" " + strings.TrimSpace(command) + " ")
	for _, token := range []string{
		" vim ", " vi ", " nano ", " emacs ", " less ", " more ",
		" top ", " htop ", " watch ", " tail -f ", " tail --follow ",
		" journalctl -f ", " journalctl --follow ",
	} {
		if strings.Contains(lower, token) {
			return true
		}
	}
	return false
}
