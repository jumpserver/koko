package terminalai

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/jumpserver/koko/pkg/srvconn"
)

type AdapterFactory func(SessionContext) Adapter

type BackgroundExecutorFactory func(
	context.Context,
	BackgroundConnection,
) (BackgroundExecutor, ProfileProvider, error)

type ProtocolRegistration struct {
	Protocol              string
	NewAdapter            AdapterFactory
	NewBackgroundExecutor BackgroundExecutorFactory
}

var protocolRegistry = struct {
	sync.RWMutex
	items map[string]ProtocolRegistration
}{items: make(map[string]ProtocolRegistration)}

func RegisterProtocol(registration ProtocolRegistration) error {
	protocol := strings.ToLower(strings.TrimSpace(registration.Protocol))
	if protocol == "" {
		return fmt.Errorf("terminal AI protocol is required")
	}
	if registration.NewAdapter == nil {
		return fmt.Errorf("terminal AI adapter factory is required for %s", protocol)
	}
	registration.Protocol = protocol
	protocolRegistry.Lock()
	defer protocolRegistry.Unlock()
	if _, exists := protocolRegistry.items[protocol]; exists {
		return fmt.Errorf("terminal AI protocol %s is already registered", protocol)
	}
	protocolRegistry.items[protocol] = registration
	return nil
}

func RegisteredProtocols() []string {
	protocolRegistry.RLock()
	result := make([]string, 0, len(protocolRegistry.items))
	for protocol := range protocolRegistry.items {
		result = append(result, protocol)
	}
	protocolRegistry.RUnlock()
	sort.Strings(result)
	return result
}

func supportsBackground(context SessionContext) bool {
	context = normalizeSessionContext(context)
	registration, ok := lookupProtocol(context.Protocol)
	if !ok || registration.NewBackgroundExecutor == nil {
		return false
	}
	adapter := ResolveAdapter(context)
	return adapter != nil && adapter.SupportsBackground()
}

func resolveBackgroundExecutor(
	ctx context.Context,
	session SessionContext,
	connection BackgroundConnection,
) (BackgroundExecutor, ProfileProvider, bool, error) {
	session = normalizeSessionContext(session)
	registration, ok := lookupProtocol(session.Protocol)
	if !ok || registration.NewBackgroundExecutor == nil {
		return nil, nil, false, nil
	}
	adapter := ResolveAdapter(session)
	if adapter == nil || !adapter.SupportsBackground() {
		return nil, nil, false, nil
	}
	if connection.Database != nil {
		connectionProtocol := strings.ToLower(strings.TrimSpace(
			connection.Database.Protocol,
		))
		if connectionProtocol != "" && connectionProtocol != session.Protocol {
			return nil, nil, true, fmt.Errorf(
				"terminal AI background protocol mismatch: session %s, connection %s",
				session.Protocol, connectionProtocol,
			)
		}
	}
	executor, provider, err := registration.NewBackgroundExecutor(ctx, connection)
	if err != nil {
		return nil, nil, true, err
	}
	if executor == nil {
		return nil, nil, true, fmt.Errorf(
			"terminal AI background executor factory returned nil for %s",
			session.Protocol,
		)
	}
	return executor, provider, true, nil
}

func lookupProtocol(protocol string) (ProtocolRegistration, bool) {
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	protocolRegistry.RLock()
	registration, ok := protocolRegistry.items[protocol]
	protocolRegistry.RUnlock()
	return registration, ok
}

func init() {
	registrations := []ProtocolRegistration{
		{
			Protocol: srvconn.ProtocolSSH,
			NewAdapter: func(context SessionContext) Adapter {
				if isShellContext(context) {
					return &shellAdapter{context: context}
				}
				return &terminalAdapter{context: context}
			},
			NewBackgroundExecutor: func(
				_ context.Context,
				connection BackgroundConnection,
			) (BackgroundExecutor, ProfileProvider, error) {
				if connection.SSHClient == nil {
					return nil, nil, fmt.Errorf("SSH background connection is unavailable")
				}
				executor := NewSSHExecutor(connection.SSHClient)
				return executor, executor, nil
			},
		},
		{
			Protocol: srvconn.ProtocolTELNET,
			NewAdapter: func(context SessionContext) Adapter {
				return &terminalAdapter{context: context}
			},
		},
		newSQLRegistration(
			srvconn.ProtocolMySQL,
			"single MySQL statement; do not use client meta-commands",
		),
		newSQLRegistration(
			srvconn.ProtocolMariadb,
			"single MariaDB SQL statement; do not use client meta-commands",
		),
		newSQLRegistration(
			srvconn.ProtocolPostgresql,
			"single PostgreSQL SQL statement; do not use psql meta-commands",
		),
		newSQLRegistration(
			srvconn.ProtocolSQLServer,
			"single Microsoft SQL Server T-SQL statement; do not use client meta-commands",
		),
		newSQLRegistration(
			srvconn.ProtocolOracle,
			"single Oracle SQL statement; do not use SQL*Plus meta-commands",
		),
		newSQLRegistration(
			srvconn.ProtocolClickHouse,
			"single ClickHouse SQL statement; do not use client meta-commands",
		),
		newRedisRegistration(),
		newMongoDBRegistration(),
	}
	for _, registration := range registrations {
		if err := RegisterProtocol(registration); err != nil {
			panic(err)
		}
	}
}

func newSQLRegistration(protocol, language string) ProtocolRegistration {
	return ProtocolRegistration{
		Protocol: protocol,
		NewAdapter: func(context SessionContext) Adapter {
			return &sqlAdapter{
				context: context, name: protocol, commandLanguage: language,
			}
		},
		NewBackgroundExecutor: databaseBackgroundExecutor,
	}
}

func newRedisRegistration() ProtocolRegistration {
	return ProtocolRegistration{
		Protocol: srvconn.ProtocolRedis,
		NewAdapter: func(context SessionContext) Adapter {
			return &redisAdapter{context: context}
		},
		NewBackgroundExecutor: databaseBackgroundExecutor,
	}
}

func newMongoDBRegistration() ProtocolRegistration {
	return ProtocolRegistration{
		Protocol: srvconn.ProtocolMongoDB,
		NewAdapter: func(context SessionContext) Adapter {
			return &mongoDBAdapter{context: context}
		},
		NewBackgroundExecutor: databaseBackgroundExecutor,
	}
}

func databaseBackgroundExecutor(
	ctx context.Context,
	connection BackgroundConnection,
) (BackgroundExecutor, ProfileProvider, error) {
	if connection.Database == nil {
		return nil, nil, fmt.Errorf("database background connection is unavailable")
	}
	config := *connection.Database
	var (
		executor BackgroundExecutor
		err      error
	)
	switch config.Protocol {
	case srvconn.ProtocolMySQL, srvconn.ProtocolMariadb:
		executor, err = NewMySQLExecutor(ctx, config)
	case srvconn.ProtocolPostgresql, srvconn.ProtocolSQLServer,
		srvconn.ProtocolOracle, srvconn.ProtocolClickHouse:
		executor, err = NewNativeSQLExecutor(ctx, config)
	case srvconn.ProtocolRedis:
		executor, err = NewRedisExecutor(ctx, config)
	case srvconn.ProtocolMongoDB:
		executor, err = NewMongoDBExecutor(ctx, config)
	default:
		err = fmt.Errorf("unsupported database background protocol %s", config.Protocol)
	}
	return executor, nil, err
}
