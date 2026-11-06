package sessiontools

import (
	"fmt"
	"strings"

	"github.com/jumpserver/koko/pkg/srvconn"
)

// AgentDatabaseCommandEligible applies the remote MCP contract without changing
// execution through an existing audited terminal.
func AgentDatabaseCommandEligible(protocol, command string) (bool, error) {
	switch protocol {
	case srvconn.ProtocolRedis:
		arguments, err := parseRedisCommand(command)
		if err != nil {
			return false, err
		}
		if !redisBackgroundEligible(arguments) {
			return false, nil
		}
		switch strings.ToUpper(arguments[0]) {
		case "READONLY", "READWRITE", "ASKING", "REPLCONF", "BLMPOP", "WAIT", "WAITAOF":
			return false, nil
		case "CLIENT":
			if len(arguments) < 2 {
				return false, nil
			}
			switch strings.ToUpper(arguments[1]) {
			case "ID", "INFO", "LIST", "GETNAME", "GETREDIR", "TRACKINGINFO", "HELP":
				return true, nil
			default:
				return false, nil
			}
		}
		return true, nil
	case srvconn.ProtocolMongoDB:
		document, err := parseMongoDBCommand(command)
		if err != nil {
			return false, err
		}
		if !mongoDBBackgroundEligible(document) {
			return false, nil
		}
		switch strings.ToLower(document[0].Key) {
		case "preparetransaction", "startsession", "endsessions", "refreshsessions":
			return false, nil
		}
		for _, field := range document {
			switch strings.ToLower(field.Key) {
			case "lsid", "txnnumber", "autocommit", "starttransaction", "stmtid", "stmtids", "txnretrycounter", "recoverytoken":
				return false, nil
			}
		}
		return true, nil
	default:
		return false, fmt.Errorf("unsupported agent database command protocol %q", protocol)
	}
}
