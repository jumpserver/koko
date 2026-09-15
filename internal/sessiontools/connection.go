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
			analysis, err := analyzeSQL(command, protocol)
			if err != nil {
				return CommandConstraints{}, err
			}
			if analysis.kind == sqlUnknown {
				return CommandConstraints{}, fmt.Errorf(
					"%s session accepts SQL statements only; operating-system shell commands are unavailable",
					protocol,
				)
			}
			if analysis.multi {
				return CommandConstraints{}, fmt.Errorf(
					"%s session tool accepts exactly one SQL statement", protocol,
				)
			}
			if analysis.incomplete {
				return CommandConstraints{}, fmt.Errorf(
					"%s SQL statement is incomplete", protocol,
				)
			}
			constraints.BackgroundEligible = analysis.BackgroundEligible()
			constraints.PTYCommand = analysis.PTYCommand(command)
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

func isSQLProtocol(protocol string) bool {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case srvconn.ProtocolMySQL, srvconn.ProtocolMariadb,
		srvconn.ProtocolPostgresql, srvconn.ProtocolSQLServer,
		srvconn.ProtocolOracle, srvconn.ProtocolClickHouse:
		return true
	default:
		return false
	}
}

func commandToolPresentation(protocol string) (title, description, commandDescription string) {
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	switch {
	case isSQLProtocol(protocol):
		label := map[string]string{
			srvconn.ProtocolMySQL: "MySQL", srvconn.ProtocolMariadb: "MariaDB",
			srvconn.ProtocolPostgresql: "PostgreSQL", srvconn.ProtocolSQLServer: "SQL Server",
			srvconn.ProtocolOracle: "Oracle", srvconn.ProtocolClickHouse: "ClickHouse",
		}[protocol]
		return "Execute " + label + " SQL",
			"Execute exactly one bounded " + label + " SQL statement against the active audited database connection. Operating-system shell commands are unavailable.",
			"Exactly one " + label + " SQL statement. Do not provide shell syntax or multiple statements. PTY execution automatically adds a missing statement-ending semicolon."
	case protocol == srvconn.ProtocolRedis:
		return "Execute Redis command",
			"Execute exactly one bounded Redis command against the active audited database connection.",
			"Exactly one Redis command using Redis command syntax."
	case protocol == srvconn.ProtocolMongoDB:
		return "Execute MongoDB command",
			"Execute exactly one bounded db.runCommand call with strict Extended JSON against the active audited MongoDB connection. This syntax is required in every execution mode. Use EJSON.deserialize with relaxed=false to preserve BSON types in both PTY and background execution; arbitrary mongosh JavaScript and shell helpers are unavailable.",
			`One db.runCommand(EJSON.deserialize(document, {"relaxed":false})) call containing a non-empty strict Extended JSON document. Double-quote all keys and string values. Use Extended JSON values instead of JavaScript constructors. A direct db.runCommand(document) is also accepted, but BSON types such as $oid, $date and $numberLong require the EJSON.deserialize wrapper in PTY mode. Collection methods, shell helpers, variables and other JavaScript expressions are unsupported.`
	case protocol == srvconn.ProtocolK8s:
		return "Execute Kubernetes shell command",
			"Execute one bounded shell command inside the active audited Kubernetes terminal.",
			"One shell command for the current Kubernetes terminal context."
	default:
		return "Execute shell command",
			"Execute one bounded shell command through the active audited terminal connection.",
			"One shell command for the active host terminal."
	}
}

func commandToolName(protocol string) string {
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	switch {
	case isSQLProtocol(protocol):
		return MCPToolExecuteSQL
	case protocol == srvconn.ProtocolRedis:
		return MCPToolExecuteRedis
	case protocol == srvconn.ProtocolMongoDB:
		return MCPToolExecuteMongoDB
	case protocol == srvconn.ProtocolSSH || protocol == srvconn.ProtocolTELNET ||
		protocol == srvconn.ProtocolK8s:
		return MCPToolExecuteShell
	default:
		return MCPToolExecuteCommand
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
