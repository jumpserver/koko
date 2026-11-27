package engine

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jumpserver/koko/pkg/chen/core"
	"github.com/jumpserver/koko/pkg/chen/dialect"
	"github.com/jumpserver/koko/pkg/chen/sqlanalysis"
	"github.com/jumpserver/koko/pkg/chen/sqlservice"
)

type Options struct {
	// SingleMode accepts configured selectors instead of Core connection tokens.
	// It must only be enabled together with the local standalone provider.
	SingleMode     bool
	StateDir       string
	AllowedOrigins []string
	// Session, console and byte quotas are optional; positive values enable them.
	MaxSessions    int
	MaxConsoles    int
	MaxResultBytes int
	MaxRows        int
	QueryTimeout   time.Duration
	// Zero uses the five-minute default; a negative value disables idle rollback.
	TransactionIdleTimeout time.Duration
}
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *APIError) Error() string { return e.Message }

type Permissions struct {
	Copy     bool `json:"can_copy"`
	Paste    bool `json:"can_paste"`
	Upload   bool `json:"can_upload"`
	Download bool `json:"can_download"`
}
type SessionView struct {
	ID               string      `json:"id"`
	Dialect          string      `json:"dialect"`
	Asset            string      `json:"asset"`
	Account          string      `json:"account"`
	Permissions      Permissions `json:"permissions"`
	ExpiresAt        time.Time   `json:"expires_at"`
	State            string      `json:"state"`
	CanCancel        bool        `json:"can_cancel"`
	StateReason      string      `json:"state_reason,omitempty"`
	CancellationMode string      `json:"cancellation_mode,omitempty"`
}
type ConsoleView struct {
	Transaction *TransactionView `json:"transaction,omitempty"`
	ID          string           `json:"id"`
	Database    string           `json:"database"`
	Schema      string           `json:"schema"`
	State       string           `json:"state"`
}
type StatementResult struct {
	ResultRole   string `json:"result_role,omitempty"`
	ResultID     string `json:"result_id,omitempty"`
	SourceSQL    string `json:"source_sql,omitempty"`
	RowsAffected string `json:"rows_affected,omitempty"`
}
type ExecutionView struct {
	ObjectPlan       *ObjectPlanOutcome     `json:"object_plan,omitempty"`
	CancelReason     string                 `json:"cancel_reason,omitempty"`
	maskedDiagnostic bool                   // the stored database error (including any script warning) is safe for AI
	Revision         uint64                 `json:"revision"`
	Confirmation     *ExecutionConfirmation `json:"confirmation,omitempty"`
	Transaction      *TransactionOutcome    `json:"transaction,omitempty"`
	QueryCount       *QueryCount            `json:"query_count,omitempty"`
	BrowseCount      *BrowseCount           `json:"browse_count,omitempty"`
	Plan             *PlanIdentity          `json:"plan,omitempty"`
	SQL              string                 `json:"sql,omitempty"` // server-generated browse SQL
	Edit             *EditOutcome           `json:"edit,omitempty"`
	ExportID         string                 `json:"export_id,omitempty"`
	ID               string                 `json:"id"`
	ConsoleID        string                 `json:"console_id"`
	Status           string                 `json:"status"`
	ResultIDs        []string               `json:"result_ids"`
	StatementResults []StatementResult      `json:"statement_results"`
	Error            *APIError              `json:"error,omitempty"`
	AuditStatus      string                 `json:"audit_status"`
	StartedAt        *time.Time             `json:"started_at,omitempty"`
	FinishedAt       *time.Time             `json:"finished_at,omitempty"`
	Review           *core.ReviewStatus     `json:"review,omitempty"`
}
type Column struct {
	Name     string `json:"name"`
	DataType string `json:"data_type"`
}
type Result struct {
	objectRole      string
	SourceSQL       string     `json:"source_sql,omitempty"` // exact server-bound statement, including script result provenance
	QueryPage       *QueryPage `json:"query_page,omitempty"`
	querySource     *queryPagePlan
	Browse          *BrowsePage `json:"browse,omitempty"`
	consoleID       string
	sourceAnalysis  *sqlanalysis.Statement
	sourceSQL       string
	browse          *editBrowse
	original        [][]any
	copyLossy       [][]bool // driver bytes rendered as hex instead of text
	masked          [][]bool
	rowRefs         []string
	editStale       bool // guarded by session.mu
	editSource      *sqlservice.EditableSource
	editSourceError string
	editSourceBound bool
	editDatabase    string
	memoryBytes     int
	ID              string   `json:"id"`
	Columns         []Column `json:"columns"`
	Rows            [][]any  `json:"rows"`
	Offset          int      `json:"offset"`
	TotalRows       int      `json:"total_rows"`
	Truncated       bool     `json:"truncated"`
	NextOffset      *int     `json:"next_offset,omitempty"`
}
type Event struct {
	Sequence uint64 `json:"sequence"`
	Type     string `json:"type"`
	Data     any    `json:"data"`
}

type execution struct {
	objectStep            int // worker-owned position in the immutable object plan
	objectPlan            *objectPlan
	objectDecisions       map[string]core.AuthorizationDecision // worker-owned, whole-plan policy outcomes
	restoreReadContext    func() error                          // worker-owned; restore before publishing source-bound reads
	consoleMode           bool                                  // raw console boundaries; ordinary query parsing is unchanged
	confirmationDecisions map[string]bool                       // protected by mu; immutable identities survive retries
	confirmationResponse  chan bool                             // protected by mu; one response per pending confirmation
	replayOutput          string                                // worker-owned, contains displayed masked values only
	auditRows             int                                   // worker-owned current statement result count
	transactionAction     string
	transactionOnPending  string
	transactionRevision   uint64
	confirmImplicitCommit bool
	queryPage             *queryPagePlan // immutable submitted operation
	querySource           *queryPagePlan // worker-owned source snapshot
	planMode              string         // immutable intent; original SQL stays in sql
	planSQL               string         // guarded by mu; omitted from small SSE snapshots
	planResult            *ExecutionPlan // guarded by mu, immutable after publication
	edit                  *editPlan      // immutable plan, consumed under console lock
	browse                *editBrowse
	resultAnalysis        *sqlanalysis.Statement
	resultSQL             string     // worker-owned original SQL for result provenance
	export                *exportJob // immutable after queue publication
	mu                    sync.RWMutex
	view                  ExecutionView
	sql                   string
	documentVersion       int // immutable editor version bound at submission
	ctx                   context.Context
	cancel                context.CancelFunc
	cancelDone            chan struct{}
	stopDatabaseWatch     func()        // protected by mu; joined before releasing the connection
	interruptBackend      func()        // one server interrupt for the current database operation
	databaseDone          chan struct{} // protected by mu; non-nil while the worker owns cancellable database work
	cancelRequested       atomic.Bool
	deadlineExpired       atomic.Bool
	transactionBoundary   bool // protected by mu
	submitReview          bool
	confirmWarnings       bool
	deadlineTimer         *time.Timer
	deadlineAt            time.Time
	remaining             time.Duration
	deadlinePaused        bool
	policyWarnings        map[string]core.AuthorizationDecision // worker-owned; original SQL -> matched policy
}

func (e *execution) snapshot() ExecutionView {
	e.mu.RLock()
	defer e.mu.RUnlock()
	v := e.view
	if v.Confirmation != nil {
		confirmation := *v.Confirmation
		v.Confirmation = &confirmation
	}
	if v.Transaction != nil {
		transaction := *v.Transaction
		v.Transaction = &transaction
	}
	if v.Plan != nil {
		identity := *v.Plan
		v.Plan = &identity
	}
	v.ResultIDs = append([]string{}, v.ResultIDs...)
	v.StatementResults = append([]StatementResult{}, v.StatementResults...)
	if v.ObjectPlan != nil {
		data := *v.ObjectPlan
		data.Steps = append([]ObjectStepOutcome(nil), data.Steps...)
		v.ObjectPlan = &data
	}
	if v.Edit != nil {
		copy := *v.Edit
		v.Edit = &copy
	}
	if v.Review != nil {
		review := *v.Review
		review.Reviewers = append([]string{}, review.Reviewers...)
		v.Review = &review
	}
	return v
}
func terminal(s string) bool {
	return s == "succeeded" || s == "failed" || s == "canceled" || s == "unknown"
}

type console struct {
	sockets             *databaseSockets // physical transports captured at login; immutable after publication
	manual              bool             // protected by mu
	manualTx            driver.Tx        // worker-owned on the pinned physical connection
	transaction         atomic.Pointer[TransactionView]
	transactionActivity time.Time // last completed SQL/grid execution, protected by mu
	id                  string
	conn                *sql.Conn
	initialMySQL        *mysqlSessionState                 // protected by mu; discarded before the first execution
	context             sqlservice.Context                 // initial connection context
	selectedContext     atomic.Pointer[sqlservice.Context] // published for SSE and AI readers
	mu                  sync.Mutex                         // physical connection: execution, metadata and context are mutually exclusive
	queue               chan *execution
	ctx                 context.Context
	cancel              context.CancelFunc
	lost                atomic.Bool
	closed              atomic.Bool
	requests            map[string]*execution // protected by session.mu
	backendID           int64
	ownedDB             *sql.DB
	catalogAuditAt      map[string]time.Time // protected by console.mu
}
type session struct {
	objectPlans    map[string]*objectPlan // guarded by mu
	editPlans      map[string]*editPlan   // protected by mu; bounded, never client-owned SQL
	exports        map[string]*exportJob
	exportRequests map[string]*exportJob
	aiTasks        map[string]*aiTask // protected by mu; bounded and canceled with the session
	catalog        *catalogResource   // protected by mu; distinct from SQL/control pools
	catalogOps     sync.WaitGroup     // Add under mu before close; includes audit completion
	workers        sync.WaitGroup     // includes closed/deleted consoles until accepted work is audited
	id             string
	grant          *core.Grant
	db             *sql.DB
	ctx            context.Context
	cancel         context.CancelFunc
	mu             sync.Mutex
	consoles       map[string]*console
	executions     map[string]*execution
	results        map[string]*Result
	resultBytes    int
	history        []Event
	sequence       uint64
	subscribers    map[uint64]chan Event
	nextSubscriber uint64
	closed         bool

	cachedExportWorkers int // active and queued cached exports, protected by mu
}

func (s *session) view() SessionView {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.viewLocked()
}
func (s *session) viewLocked() SessionView {
	state := "ready"
	reason := ""
	if s.closed {
		state = "closed"
		reason = "session_closed"
	} else if err := s.grant.Check(); err != nil {
		state = "locked"
		reason = err.Error()
	}
	p := s.grant.Permissions
	d := s.grant.Dialect()
	mode := ""
	if d == dialect.Dameng || d == dialect.DB2 {
		mode = "connection"
	}
	return SessionView{s.id, string(d), s.grant.AssetName, s.grant.AccountName, Permissions{p.Copy, p.Paste, p.Upload, p.Download}, s.grant.ExpiresAt, state, supportsActiveCancellation(d), reason, mode}
}
func (s *session) emit(kind string, data any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sequence++
	ev := Event{s.sequence, kind, data}
	s.history = append(s.history, ev)
	if len(s.history) > 256 {
		s.history = s.history[len(s.history)-256:]
	}
	for id, ch := range s.subscribers {
		select {
		case ch <- ev:
		default:
			close(ch)
			delete(s.subscribers, id)
		}
	}
}
func tokenKey(token string) [32]byte { return sha256.Sum256([]byte(token)) }
func newID(prefix string) string {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return prefix + hex.EncodeToString(b[:])
}

type creation struct {
	done       chan struct{}
	token      string
	session    *session
	err        error
	requestKey [32]byte // hash of the browser's stable, random create nonce
	// Completed failures and closed-session tombstones retain only a bounded
	// idempotency outcome. Active sessions keep their creation entry until close.
	expiresAt time.Time
}
type Server struct {
	exportDir     string
	exportLock    *os.File
	exportInitErr error
	exportSlots   chan struct{}
	downloads     map[[32]byte]downloadTicket // protected by mu; only hashed one-use capabilities
	catalogSlots  chan struct{}               // global catalog-only concurrency limit
	provider      core.Provider
	opts          Options
	mu            sync.RWMutex
	sessions      map[[32]byte]*session
	creations     map[[32]byte]*creation
	ctx           context.Context
	cancel        context.CancelFunc
	creating      sync.WaitGroup
	cleanup       sync.WaitGroup
	cleanupErr    error // protected by mu
	shutdownOnce  sync.Once
	shutdownDone  chan struct{}
}
