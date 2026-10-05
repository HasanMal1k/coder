package store

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Agent is a coding agent: a named conversation with the agent program
// inside a machine, belonging to a chat.
type Agent struct {
	ID         string
	OwnerID    string
	ChatID     string
	InstanceID string
	// MachineName is the instance's name. It is read with the agent, never
	// stored on it.
	MachineName string
	// ChatTitle is the title of the chat the agent belongs to, read with it.
	ChatTitle  string
	Name       string
	Status     string
	Task       string
	Background bool
	Activity   string
	Report     string
	StartedAt  *time.Time
	FinishedAt *time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

const agentColumns = `a.id, a.owner_id, a.chat_id, a.instance_id, i.name, c.title, a.name, a.status,
	a.task, a.background, a.activity, a.report, a.started_at, a.finished_at, a.created_at, a.updated_at`

const agentTables = `agents a JOIN instances i ON i.id = a.instance_id JOIN chats c ON c.id = a.chat_id`

// AgentTask is one task an agent was given, with what it did and how it
// ended.
type AgentTask struct {
	AgentID    string
	Task       string
	Background bool
	// Status is "working", "done", "failed" or "stopped".
	Status string
	// Log is what the agent did, a line for each step.
	Log        []string
	Report     string
	StartedAt  time.Time
	FinishedAt *time.Time
}

// AgentNote is something an agent wrote down for the others.
type AgentNote struct {
	OwnerID   string
	Machine   string
	Author    string
	Topic     string
	Body      string
	CreatedAt time.Time
}

// maxTaskLog caps the steps recorded for one task.
const maxTaskLog = 300

// maxAgentsListed caps how many agents one listing returns. Those working
// come first, so the cap only ever cuts off old, finished ones.
const maxAgentsListed = 100

type AgentRepository struct {
	db *pgxpool.Pool
}

func NewAgentRepository(db *pgxpool.Pool) *AgentRepository {
	return &AgentRepository{
		db: db,
	}
}

// Ensure returns the chat's agent with the given name on an instance,
// creating it, idle, if there is none.
func (r *AgentRepository) Ensure(ctx context.Context, ownerID, chatID, instanceID, name string) (*Agent, error) {
	// The update that changes nothing is what makes RETURNING yield the row
	// that was already there.
	var id string
	err := r.db.QueryRow(ctx,
		`INSERT INTO agents (id, owner_id, chat_id, instance_id, name, status)
		 VALUES ($1, $2, $3, $4, $5, 'idle')
		 ON CONFLICT (chat_id, instance_id, name) DO UPDATE SET name = EXCLUDED.name
		 RETURNING id`,
		uuid.NewString(), ownerID, chatID, instanceID, name,
	).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("ensure agent: %w", err)
	}

	return r.GetByID(ctx, id, ownerID)
}

// GetByID returns the agent only if it belongs to ownerID.
func (r *AgentRepository) GetByID(ctx context.Context, id, ownerID string) (*Agent, error) {
	rows, err := r.db.Query(ctx,
		`SELECT `+agentColumns+`
		 FROM `+agentTables+`
		 WHERE a.id = $1 AND a.owner_id = $2`,
		id, ownerID,
	)
	if err != nil {
		return nil, fmt.Errorf("get agent: %w", err)
	}

	agent, err := pgx.CollectExactlyOneRow(rows, scanAgent)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get agent: %w", err)
	}

	return agent, nil
}

// ListByOwner returns the owner's agents: those working first, then the most
// recently active.
func (r *AgentRepository) ListByOwner(ctx context.Context, ownerID string) ([]*Agent, error) {
	return r.list(ctx,
		`WHERE a.owner_id = $1
		 ORDER BY (a.status = 'working') DESC, a.updated_at DESC
		 LIMIT `+fmt.Sprint(maxAgentsListed),
		ownerID,
	)
}

// ListByChat returns a chat's agents, oldest first.
func (r *AgentRepository) ListByChat(ctx context.Context, chatID string) ([]*Agent, error) {
	return r.list(ctx, `WHERE a.chat_id = $1 ORDER BY a.created_at`, chatID)
}

// ListByStatus returns every agent in the given status, whoever owns it. It
// is for background work, never for answering a user's request.
func (r *AgentRepository) ListByStatus(ctx context.Context, status string) ([]*Agent, error) {
	return r.list(ctx, `WHERE a.status = $1`, status)
}

func (r *AgentRepository) list(ctx context.Context, where string, arg any) ([]*Agent, error) {
	rows, err := r.db.Query(ctx, `SELECT `+agentColumns+` FROM `+agentTables+` `+where, arg)
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}

	agents, err := pgx.CollectRows(rows, scanAgent)
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}

	return agents, nil
}

// Begin gives the agent a task, unless it is already working on one, and
// reports whether it did. The check and the change are one statement, so of
// two tasks given to an agent at once, exactly one is accepted.
func (r *AgentRepository) Begin(ctx context.Context, id, task string, background bool) (bool, error) {
	// The task is added to the agent's history in the same statement, and
	// only if the claim succeeded.
	tag, err := r.db.Exec(ctx,
		`WITH claimed AS (
		     UPDATE agents
		     SET status = 'working', task = $2, background = $3, activity = '', report = '',
		         started_at = NOW(), finished_at = NULL, updated_at = NOW()
		     WHERE id = $1 AND status <> 'working'
		     RETURNING id
		 )
		 INSERT INTO agent_tasks (agent_id, task, background, status)
		 SELECT id, $2, $3, 'working' FROM claimed`,
		id, task, background,
	)
	if err != nil {
		return false, fmt.Errorf("begin agent task: %w", err)
	}

	return tag.RowsAffected() == 1, nil
}

// Finish records how the agent's task ended: its new status, and its report
// or the reason it failed.
func (r *AgentRepository) Finish(ctx context.Context, id, status, report string) error {
	// An agent that finished is idle again; the task it finished is done.
	taskStatus := status
	if status == "idle" {
		taskStatus = "done"
	}

	_, err := r.db.Exec(ctx,
		`WITH finished AS (
		     UPDATE agents
		     SET status = $2, report = $3, activity = '', finished_at = NOW(), updated_at = NOW()
		     WHERE id = $1
		 )
		 UPDATE agent_tasks
		 SET status = $4, report = $3, finished_at = NOW()
		 WHERE agent_id = $1 AND status = 'working'`,
		id, status, report, taskStatus,
	)
	if err != nil {
		return fmt.Errorf("finish agent task: %w", err)
	}

	return nil
}

// SetActivity records what the agent is doing right now.
func (r *AgentRepository) SetActivity(ctx context.Context, id, activity string) error {
	// Also added to the log of the task in progress, up to a limit.
	_, err := r.db.Exec(ctx,
		`WITH now_doing AS (
		     UPDATE agents SET activity = $2, updated_at = NOW() WHERE id = $1
		 )
		 UPDATE agent_tasks
		 SET log = array_append(log, $2)
		 WHERE agent_id = $1 AND status = 'working' AND cardinality(log) < $3`,
		id, activity, maxTaskLog,
	)
	if err != nil {
		return fmt.Errorf("set agent activity: %w", err)
	}

	return nil
}

// ListTasks returns the latest tasks of each of the owner's agents, at most
// perAgent each, oldest first within an agent.
func (r *AgentRepository) ListTasks(ctx context.Context, ownerID string, perAgent int) ([]*AgentTask, error) {
	rows, err := r.db.Query(ctx,
		`SELECT agent_id, task, background, status, log, report, started_at, finished_at
		 FROM (
		     SELECT t.*, row_number() OVER (PARTITION BY t.agent_id ORDER BY t.id DESC) AS recency
		     FROM agent_tasks t
		     JOIN agents a ON a.id = t.agent_id
		     WHERE a.owner_id = $1
		 ) latest
		 WHERE recency <= $2
		 ORDER BY agent_id, id`,
		ownerID, perAgent,
	)
	if err != nil {
		return nil, fmt.Errorf("list agent tasks: %w", err)
	}

	tasks, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (*AgentTask, error) {
		var t AgentTask
		if err := row.Scan(&t.AgentID, &t.Task, &t.Background, &t.Status, &t.Log, &t.Report, &t.StartedAt, &t.FinishedAt); err != nil {
			return nil, err
		}
		return &t, nil
	})
	if err != nil {
		return nil, fmt.Errorf("list agent tasks: %w", err)
	}

	return tasks, nil
}

// AddNote records something an agent wrote down for the others.
func (r *AgentRepository) AddNote(ctx context.Context, note *AgentNote) error {
	err := r.db.QueryRow(ctx,
		`INSERT INTO agent_notes (owner_id, machine, author, topic, body)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING created_at`,
		note.OwnerID, note.Machine, note.Author, note.Topic, note.Body,
	).Scan(&note.CreatedAt)
	if err != nil {
		return fmt.Errorf("add agent note: %w", err)
	}

	return nil
}

// ListNotes returns the owner's latest notes, at most limit, oldest first.
func (r *AgentRepository) ListNotes(ctx context.Context, ownerID string, limit int) ([]*AgentNote, error) {
	rows, err := r.db.Query(ctx,
		`SELECT owner_id, machine, author, topic, body, created_at
		 FROM (
		     SELECT * FROM agent_notes WHERE owner_id = $1 ORDER BY id DESC LIMIT $2
		 ) latest
		 ORDER BY id`,
		ownerID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list agent notes: %w", err)
	}

	notes, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (*AgentNote, error) {
		var n AgentNote
		if err := row.Scan(&n.OwnerID, &n.Machine, &n.Author, &n.Topic, &n.Body, &n.CreatedAt); err != nil {
			return nil, err
		}
		return &n, nil
	})
	if err != nil {
		return nil, fmt.Errorf("list agent notes: %w", err)
	}

	return notes, nil
}

// Session returns the agent program's ID for this agent's conversation, or
// ErrNotFound if it has none yet.
func (r *AgentRepository) Session(ctx context.Context, id string) (string, error) {
	var sessionID *string

	err := r.db.QueryRow(ctx, `SELECT session_id FROM agents WHERE id = $1`, id).Scan(&sessionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("get agent session: %w", err)
	}
	if sessionID == nil {
		return "", ErrNotFound
	}

	return *sessionID, nil
}

// SetSession records the agent program's ID for this agent's conversation,
// replacing any earlier one.
func (r *AgentRepository) SetSession(ctx context.Context, id, sessionID string) error {
	_, err := r.db.Exec(ctx, `UPDATE agents SET session_id = $2 WHERE id = $1`, id, sessionID)
	if err != nil {
		return fmt.Errorf("set agent session: %w", err)
	}

	return nil
}

func scanAgent(row pgx.CollectableRow) (*Agent, error) {
	var a Agent
	err := row.Scan(&a.ID, &a.OwnerID, &a.ChatID, &a.InstanceID, &a.MachineName, &a.ChatTitle, &a.Name, &a.Status,
		&a.Task, &a.Background, &a.Activity, &a.Report, &a.StartedAt, &a.FinishedAt, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// AgentMessage is a question one agent left for another that was busy.
type AgentMessage struct {
	FromMachine string
	FromAgent   string
	Body        string
	CreatedAt   time.Time
}

// AddMessage leaves a message for an agent.
func (r *AgentRepository) AddMessage(ctx context.Context, toAgentID string, msg *AgentMessage) error {
	_, err := r.db.Exec(ctx,
		`INSERT INTO agent_messages (to_agent_id, from_machine, from_agent, body)
		 VALUES ($1, $2, $3, $4)`,
		toAgentID, msg.FromMachine, msg.FromAgent, msg.Body,
	)
	if err != nil {
		return fmt.Errorf("add agent message: %w", err)
	}

	return nil
}

// TakeMessages returns the messages waiting for an agent, oldest first, and
// marks them as delivered, in one statement: a message is handed over once.
func (r *AgentRepository) TakeMessages(ctx context.Context, agentID string) ([]*AgentMessage, error) {
	rows, err := r.db.Query(ctx,
		`UPDATE agent_messages
		 SET delivered_at = NOW()
		 WHERE to_agent_id = $1 AND delivered_at IS NULL
		 RETURNING id, from_machine, from_agent, body, created_at`,
		agentID,
	)
	if err != nil {
		return nil, fmt.Errorf("take agent messages: %w", err)
	}

	type numbered struct {
		id  int64
		msg *AgentMessage
	}
	taken, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (numbered, error) {
		n := numbered{msg: &AgentMessage{}}
		err := row.Scan(&n.id, &n.msg.FromMachine, &n.msg.FromAgent, &n.msg.Body, &n.msg.CreatedAt)
		return n, err
	})
	if err != nil {
		return nil, fmt.Errorf("take agent messages: %w", err)
	}

	// RETURNING promises no order.
	slices.SortFunc(taken, func(a, b numbered) int { return cmp.Compare(a.id, b.id) })

	messages := make([]*AgentMessage, len(taken))
	for i, n := range taken {
		messages[i] = n.msg
	}

	return messages, nil
}
