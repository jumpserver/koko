package sessiontools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func TestPostgreSQLCommandToolContract(t *testing.T) {
	validator := ProtocolCommandValidator("postgresql")
	if _, err := validator("free -h"); err == nil || !strings.Contains(err.Error(), "SQL statements only") {
		t.Fatalf("shell command validation error = %v", err)
	}
	if _, err := validator("SELECT current_setting('shared_buffers')"); err != nil {
		t.Fatalf("valid PostgreSQL statement rejected: %v", err)
	}

	handler, err := NewCommandTool(MCPCommandToolOptions{
		Protocol: "postgresql", Validate: validator,
		Hooks: MCPCommandHooks{PTYExecute: func(
			context.Context, string, *CommandACLDecision,
		) (string, *int, error) {
			return "", nil, nil
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	definition := handler.Definition()
	if definition.Name != MCPToolExecuteSQL || definition.Title != "Execute PostgreSQL SQL" ||
		!strings.Contains(definition.Description, "shell commands are unavailable") {
		t.Fatalf("unexpected PostgreSQL tool presentation: %#v", definition)
	}
	properties := definition.InputSchema["properties"].(map[string]any)
	patternValue := properties["command"].(map[string]any)["pattern"]
	pattern, ok := patternValue.(string)
	if !ok {
		t.Fatalf("command schema pattern = %#v", patternValue)
	}
	compiledPattern, err := regexp.Compile(pattern)
	if err != nil {
		t.Fatalf("compile command schema pattern: %v", err)
	}
	if !compiledPattern.MatchString("SELECT 1") {
		t.Fatal("command schema rejected a valid single-line command")
	}
	for _, command := range []string{
		"SELECT 1\nSELECT 2",
		"\tSELECT 1",
		"SELECT 1\u0085",
		"SELECT 1\u2028SELECT 2",
		"SELECT 1\u2029SELECT 2",
	} {
		if compiledPattern.MatchString(command) {
			t.Fatalf("command schema accepted %q", command)
		}
		payload, marshalErr := json.Marshal(map[string]string{"command": command})
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		if _, callErr := handler.Call(context.Background(), payload); callErr == nil ||
			!strings.Contains(callErr.Error(), "line breaks or control characters") {
			t.Fatalf("command %q validation error = %v", command, callErr)
		}
	}
	modes := properties["execution"].(map[string]any)["enum"].([]string)
	if len(modes) != 2 || modes[0] != MCPExecutionAuto || modes[1] != MCPExecutionPTY {
		t.Fatalf("execution modes = %#v", modes)
	}
}

func TestDatabaseSchemaArgumentsSupportBoundedListing(t *testing.T) {
	for _, request := range []SQLSchemaLookupRequest{{}, {Query: "*"}} {
		normalized, err := normalizeSQLSchemaLookupRequest(request)
		if err != nil {
			t.Fatal(err)
		}
		if normalized.Query != "" || len(normalized.Tables) != 0 {
			t.Fatalf("normalized request = %#v", normalized)
		}
	}
}

func TestSQLExecutorDefersConnection(t *testing.T) {
	for _, protocol := range []string{"mysql", "mariadb", "postgresql", "sqlserver", "oracle", "clickhouse"} {
		t.Run(protocol, func(t *testing.T) {
			executor, err := NewDatabaseExecutor(context.Background(), DatabaseConfig{
				Protocol: protocol, Host: "127.0.0.1", Port: 1, Database: "test",
			})
			if err != nil {
				t.Fatalf("initialization should not require a live database: %v", err)
			}
			defer executor.Close()
		})
	}
}

func TestMongoDBCommandContract(t *testing.T) {
	validator := ProtocolCommandValidator("mongodb")
	for _, command := range []string{
		`db.runCommand({"ping":1})`,
		`db.runCommand({"find":"logs","filter":{"_id":{"$oid":"507f1f77bcf86cd799439011"}}})`,
		`db.runCommand(EJSON.deserialize({"ping":1}, {"relaxed":false}))`,
	} {
		if _, err := validator(command); err != nil {
			t.Fatalf("valid MongoDB command rejected: %v", err)
		}
	}
	for _, command := range []string{
		`db.logs.find({})`, `show collections`, `db.runCommand({ping:1})`,
		`db.runCommand({"find":"logs","filter":{"_id":ObjectId("507f1f77bcf86cd799439011")}})`,
		`db.runCommand(EJSON.deserialize({"ping":1}, {"relaxed":true}))`,
		`db.runCommand(EJSON.deserialize({"ping":1}, {"relaxed":false})); db.dropDatabase()`,
	} {
		if _, err := validator(command); err == nil {
			t.Fatalf("unsupported MongoDB syntax accepted: %s", command)
		}
	}
	document, err := parseMongoDBCommand(`db.runCommand(EJSON.deserialize({"find":"logs","filter":{"_id":{"$oid":"507f1f77bcf86cd799439011"},"sequence":{"$numberLong":"9007199254740993"}}}, {"relaxed":false}))`)
	if err != nil {
		t.Fatal(err)
	}
	filter := document[1].Value.(bson.D)
	if filter[0].Value.(primitive.ObjectID).Hex() != "507f1f77bcf86cd799439011" || filter[1].Value != int64(9007199254740993) {
		t.Fatalf("BSON types were not preserved: %#v", filter)
	}
	_, description, commandDescription := commandToolPresentation("mongodb")
	if !strings.Contains(description, "every execution mode") ||
		!strings.Contains(commandDescription, "db.runCommand") ||
		!strings.Contains(commandDescription, "strict Extended JSON") ||
		!strings.Contains(commandDescription, "EJSON.deserialize") {
		t.Fatal("MongoDB tool description does not expose its syntax requirements")
	}
}

func TestSQLExecuteCommandContract(t *testing.T) {
	for _, tc := range []struct{ protocol, command string }{
		{"sqlserver", "EXEC dbo.usp_report @id = 1"},
		{"sqlserver", "execute sys.sp_executesql N'SELECT 1; SELECT 2;'"},
		{"postgresql", "EXECUTE prepared_query(42);"},
		{"mysql", "EXECUTE prepared_statement"},
	} {
		constraints, err := ProtocolCommandValidator(tc.protocol)(tc.command)
		if err != nil || constraints.BackgroundEligible {
			t.Fatalf("procedure or prepared statement must use the active session: %+v, %v", constraints, err)
		}
	}
	for _, command := range []string{"EXEC dbo.usp_report; SELECT 1", "EXEC ('SELECT 1)", "execfile /tmp/task"} {
		if _, err := ProtocolCommandValidator("sqlserver")(command); err == nil {
			t.Fatalf("invalid SQL execution accepted: %s", command)
		}
	}
}

type commandCaptureExecutor struct{ command string }

func (e *commandCaptureExecutor) Execute(_ context.Context, command string, _ func(string)) (string, *int, error) {
	e.command = command
	return "", nil, nil
}

func (*commandCaptureExecutor) Close() error { return nil }

func TestCommandToolPTYNormalization(t *testing.T) {
	for _, tc := range []struct{ protocol, mode, command, want, execution string }{
		{"postgresql", "pty", "SELECT 1", "SELECT 1;", "pty"},
		{"postgresql", "pty", "SELECT 1; -- done", "SELECT 1; -- done", "pty"},
		{"postgresql", "pty", "SELECT ';' -- done;", "SELECT ';'; -- done;", "pty"},
		{"postgresql", "pty", "SELECT 1 /* ; */", "SELECT 1 /* ; */;", "pty"},
		{"mysql", "pty", "SELECT 1 # done;", "SELECT 1; # done;", "pty"},
		{"clickhouse", "pty", "SELECT 1 // done;", "SELECT 1; // done;", "pty"},
		{"postgresql", "auto", "SET search_path TO public", "SET search_path TO public;", "pty"},
		{"postgresql", "auto", "SELECT 1", "SELECT 1", "background"},
		{"postgresql", "background", "SELECT 1", "SELECT 1", "background"},
		{"ssh", "pty", "pwd", "pwd", "pty"},
		{"postgresql", "pty", "SELECT 1 --" + strings.Repeat("x", maximumMCPCommandSize-len("SELECT 1 --")), "", ""},
	} {
		t.Run(tc.protocol+"/"+tc.mode+"/"+tc.execution, func(t *testing.T) {
			executor := &commandCaptureExecutor{}
			var ptyCommand, reviewCommand string
			handler, err := NewCommandTool(MCPCommandToolOptions{
				Protocol: tc.protocol, Executor: executor, Validate: ProtocolCommandValidator(tc.protocol),
				Hooks: MCPCommandHooks{
					BackgroundAvailable: func() bool { return true },
					CommandACLCheck: func(command string) CommandACLDecision {
						if command != tc.want {
							t.Fatal("ACL checked an unexpected command")
						}
						return CommandACLDecision{Action: "review"}
					},
					CommandACLReview: func(_ context.Context, decision CommandACLDecision, command string, _ func(CommandACLDecision)) (CommandACLDecision, error) {
						reviewCommand = command
						decision.Action, decision.Reviewed = "accept", true
						return decision, nil
					},
					PTYExecute: func(_ context.Context, command string, _ *CommandACLDecision) (string, *int, error) {
						ptyCommand = command
						return "", nil, nil
					},
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			arguments, _ := json.Marshal(mcpCommandArguments{Command: tc.command, Execution: tc.mode})
			value, err := handler.Call(context.Background(), arguments)
			if tc.want == "" {
				if err == nil || !strings.Contains(err.Error(), "after PTY normalization") {
					t.Fatalf("expected normalization length error, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			result := value.(MCPCommandResult)
			executed := executor.command
			if tc.execution == "pty" {
				executed = ptyCommand
			}
			if executed != tc.want || reviewCommand != tc.want || result.Command != tc.want || result.Execution != tc.execution {
				t.Fatalf("executed %q, reviewed %q, result %+v", executed, reviewCommand, result)
			}
		})
	}
}

func TestSQLDialectBoundaries(t *testing.T) {
	for _, tc := range []struct {
		protocol, command   string
		background, invalid bool
	}{
		{"sqlserver", `SELECT [order; id] FROM dbo.orders`, true, false},
		{"sqlserver", `SELECT N'C:\'`, true, false},
		{"sqlserver", `SELECT * FROM #items`, false, false},
		{"sqlserver", `SELECT * FROM [#items]`, false, false},
		{"sqlserver", `SELECT * FROM "#items"`, false, false},
		{"sqlserver", `SELECT * FROM #items; DROP TABLE items`, false, true},
		{"sqlserver", `SELECT [unclosed`, false, true},
		{"postgresql", `SELECT $tag$a'; SELECT 2$tag$`, true, false},
		{"postgresql", `SELECT E'a\';b'`, true, false},
		{"postgresql", `SELECT 'C:\'`, true, false},
		{"postgresql", `SELECT 1--comment`, true, false},
		{"postgresql", `SELECT 1 /* outer /* inner */ outer */`, true, false},
		{"postgresql", `SELECT 1; $$hidden$$`, false, true},
		{"postgresql", `SELECT $tag$unclosed`, false, true},
		{"mysql", `SELECT 1--not-a-comment; SELECT 2`, false, true},
		{"mysql", `SELECT 1 # ignored; SELECT 2`, true, false},
		{"mysql", `SELECT 1 /*! INTO OUTFILE '/tmp/data' */`, false, true},
		{"mysql", `SELECT (1`, false, true},
		{"oracle", `SELECT q'[a';b]' FROM dual`, true, false},
		{"oracle", `SELECT q'[unclosed' FROM dual`, false, true},
		{"clickhouse", `SELECT $tag$a';b$tag$ // comment`, true, false},
		{"postgresql", `EXEC report`, false, true},
		{"sqlserver", `PREPARE report FROM 'SELECT 1'`, false, true},
	} {
		constraints, err := ProtocolCommandValidator(tc.protocol)(tc.command)
		if (err != nil) != tc.invalid || (err == nil && constraints.BackgroundEligible != tc.background) {
			t.Errorf("%s %q: constraints=%+v, error=%v", tc.protocol, tc.command, constraints, err)
		}
	}
}

func TestSQLDialectCommands(t *testing.T) {
	for _, tc := range []struct {
		protocol, command string
		background        bool
	}{
		{"mysql", `CALL report(1)`, false},
		{"mysql", `PREPARE report FROM 'SELECT 1'`, false},
		{"mariadb", `DEALLOCATE PREPARE report`, false},
		{"postgresql", `CALL report(1)`, false},
		{"postgresql", `PREPARE report AS SELECT 1`, false},
		{"postgresql", `DEALLOCATE report`, false},
		{"oracle", `CALL report(1)`, false},
		{"postgresql", `RESET search_path`, false},
		{"mysql", `RESET REPLICA`, true},
		{"postgresql", `VALUES (1), (2)`, true},
		{"postgresql", `CREATE TEMP TABLE items(id int)`, false},
		{"sqlserver", `DBCC CHECKDB`, false},
		{"sqlserver", `MERGE items USING src ON items.id=src.id WHEN MATCHED THEN UPDATE SET value=src.value;`, true},
		{"sqlserver", `INSERT INTO items(id) OUTPUT inserted.id VALUES(1)`, false},
		{"postgresql", `INSERT INTO items(id) VALUES(1) RETURNING id`, false},
		{"clickhouse", `EXISTS TABLE items`, true},
		{"clickhouse", `CHECK TABLE items`, true},
		{"clickhouse", `SYSTEM FLUSH LOGS`, true},
	} {
		constraints, err := ProtocolCommandValidator(tc.protocol)(tc.command)
		if err != nil || constraints.BackgroundEligible != tc.background {
			t.Errorf("%s %q: constraints=%+v, error=%v", tc.protocol, tc.command, constraints, err)
		}
	}
}

func TestSessionSpecificToolSelection(t *testing.T) {
	for protocol, expected := range map[string]string{
		"ssh":        MCPToolExecuteShell,
		"k8s":        MCPToolExecuteShell,
		"postgresql": MCPToolExecuteSQL,
		"redis":      MCPToolExecuteRedis,
		"mongodb":    MCPToolExecuteMongoDB,
	} {
		if actual := commandToolName(protocol); actual != expected {
			t.Fatalf("command tool for %s = %s, want %s", protocol, actual, expected)
		}
	}

	handlers, err := NewFileToolHandlers(
		struct{ FileExecutor }{},
		FileToolCapabilities{ReadText: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(handlers))
	for _, handler := range handlers {
		names = append(names, handler.Definition().Name)
	}
	joined := strings.Join(names, ",")
	if joined != "list_directory,stat,read_text" {
		t.Fatalf("read-only file tools = %s", joined)
	}
}

func TestShellCommandToolDeclaresKaelReadOnlyPolicy(t *testing.T) {
	for _, protocol := range []string{"ssh", "telnet", "k8s", "local-shell"} {
		handler, err := NewCommandTool(MCPCommandToolOptions{
			Protocol: protocol, Validate: func(string) (CommandConstraints, error) { return CommandConstraints{}, nil },
			Hooks: MCPCommandHooks{PTYExecute: func(context.Context, string, *CommandACLDecision) (string, *int, error) {
				return "", nil, nil
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if handler.Definition().Meta[MCPCommandPolicyMetaKey] != MCPShellReadOnlyPolicy {
			t.Fatalf("%s command tool did not declare the Kael shell policy", protocol)
		}
	}

	handler, err := NewCommandTool(MCPCommandToolOptions{
		Protocol: "postgresql", Validate: func(string) (CommandConstraints, error) { return CommandConstraints{}, nil },
		Hooks: MCPCommandHooks{PTYExecute: func(context.Context, string, *CommandACLDecision) (string, *int, error) {
			return "", nil, nil
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := handler.Definition().Meta[MCPCommandPolicyMetaKey]; ok {
		t.Fatal("database command tool declared a shell policy")
	}
}

func TestAgentToolOutputSchemasAndLargeResult(t *testing.T) {
	schemas := []map[string]any{
		commandOutputSchema(), terminalContextOutputSchema(),
		terminalSnapshotOutputSchema(), databaseSchemaOutputSchema(),
	}
	for _, name := range []string{
		ToolListDirectory, ToolStat, ToolReadText, ToolSaveText,
		ToolMkdir, ToolRename, ToolDelete,
	} {
		schemas = append(schemas, fileToolOutputSchema(name))
	}
	for _, schema := range schemas {
		encoded, err := json.Marshal(schema)
		if err != nil {
			t.Fatal(err)
		}
		if err = ValidateSchema(encoded); err != nil {
			t.Fatalf("invalid output schema: %v", err)
		}
	}

	result, payload := newMCPCallToolResult(map[string]any{
		"output": strings.Repeat("x", 100*1024),
	}, nil)
	if len(payload) == 0 || len(payload) > MaxToolResultBytes {
		t.Fatalf("large structured result has invalid wire size %d", len(payload))
	}
	if result.StructuredContent == nil || len(result.Content) != 1 ||
		len(result.Content[0].Text) > maxMCPTextResultBytes {
		t.Fatal("large structured result was not preserved with a bounded text fallback")
	}
}

func TestMCPAgentBindingRequiresRevision(t *testing.T) {
	meta := map[string]json.RawMessage{
		MCPAgentMetaKey: json.RawMessage(`{"resource_session_id":"resource-1","tool_call_id":"call-1"}`),
		"io.modelcontextprotocol/protocolVersion":    json.RawMessage(`"2026-07-28"`),
		"io.modelcontextprotocol/clientCapabilities": json.RawMessage(`{}`),
	}
	if _, err := decodeMCPAgentBinding(meta, true); err == nil {
		t.Fatal("binding without a toolset revision was accepted")
	}
}

func TestMCPAgentBindingIgnoresUnknownFields(t *testing.T) {
	meta := map[string]json.RawMessage{
		MCPAgentMetaKey: json.RawMessage(`{"resource_session_id":"resource-1","tool_call_id":"call-1","revision":1,"registration_id":"registration-1","invocation_id":"invocation-1","definition_version":"1","definition_digest":"digest-1"}`),
		"io.modelcontextprotocol/protocolVersion":    json.RawMessage(`"2026-07-28"`),
		"io.modelcontextprotocol/clientCapabilities": json.RawMessage(`{}`),
	}
	binding, err := decodeMCPAgentBinding(meta, true)
	if err != nil {
		t.Fatal(err)
	}
	if binding.ResourceSessionID != "resource-1" || binding.ToolCallID != "call-1" || binding.Revision != 1 {
		t.Fatalf("unexpected binding: %#v", binding)
	}
}

func TestCommandReviewProgress(t *testing.T) {
	for _, action := range []string{"accept", "reject", "cancel"} {
		t.Run(action, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			approval := make(chan struct{})
			frames := make(chan MCPOutbound, 4)
			executor := &commandCaptureExecutor{}
			handler, err := NewCommandTool(MCPCommandToolOptions{
				Executor: executor, Validate: ProtocolCommandValidator("ssh"),
				Hooks: MCPCommandHooks{
					BackgroundAvailable: func() bool { return true },
					CommandACLCheck:     func(string) CommandACLDecision { return CommandACLDecision{Action: "review"} },
					CommandACLReview: func(ctx context.Context, decision CommandACLDecision, _ string, progress func(CommandACLDecision)) (CommandACLDecision, error) {
						progress(decision)
						select {
						case <-approval:
							decision.Action, decision.Reviewed = action, true
							if action == "cancel" {
								decision.Action = "accept"
							}
							return decision, nil
						case <-ctx.Done():
							return decision, ctx.Err()
						}
					},
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			dispatcher, err := NewMCPDispatcher(ctx, MCPDispatcherOptions{
				ResourceSessionID: "session", Profile: "terminal", Handlers: []MCPToolHandler{handler},
				Emit: func(frame MCPOutbound) { frames <- frame },
			})
			if err != nil {
				t.Fatal(err)
			}
			defer dispatcher.Close()
			request := `{"jsonrpc":"2.0","id":"call","method":"tools/call","params":{"name":"execute_command","arguments":{"command":"ls"},"_meta":{"io.modelcontextprotocol/protocolVersion":"` + MCPWireVersion + `","io.modelcontextprotocol/clientCapabilities":{},"com.jumpserver/agent":{"resource_session_id":"session","tool_call_id":"call","revision":1}}}}`
			if err = dispatcher.HandleRequest([]byte(request)); err != nil {
				t.Fatal(err)
			}
			receive := func(kind string, sequence uint64) MCPResponse {
				t.Helper()
				select {
				case frame := <-frames:
					var response MCPResponse
					if err := json.Unmarshal(frame.Data, &response); err != nil || frame.Type != kind || response.Sequence != sequence {
						t.Fatalf("unexpected frame: %s %s, %v", frame.Type, frame.Data, err)
					}
					return response
				case <-time.After(time.Second):
					t.Fatal("missing executor response")
					return MCPResponse{}
				}
			}
			pending := receive(MCPProgressFrame, 1)
			if pending.Result.(map[string]any)["status"] != "awaiting_approval" || executor.command != "" {
				t.Fatal("command must remain unexecuted while review is pending")
			}
			if action == "cancel" {
				cancel()
			}
			close(approval)
			sequence := uint64(2)
			if action == "accept" {
				receive(MCPProgressFrame, sequence)
				sequence++
			}
			result := receive(MCPResponseFrame, sequence).Result.(map[string]any)
			if (executor.command != "") != (action == "accept") || (result["isError"] == true) != (action != "accept") {
				t.Fatalf("unexpected command review outcome: %q %+v", executor.command, result)
			}
		})
	}
}

func TestMCPToolTimeoutAndCancellation(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status string
	}{{context.DeadlineExceeded, "timeout"}, {context.Canceled, "cancelled"}} {
		result, payload := newMCPCallToolResult(nil, fmt.Errorf("execute: %w", tc.err))
		meta, ok := result.Meta[MCPAgentMetaKey].(map[string]any)
		if !result.IsError || !ok || meta["status"] != tc.status || !json.Valid(payload) {
			t.Fatalf("missing outcome %s: %+v", tc.status, result)
		}
	}
}
