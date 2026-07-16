package mock

import (
	"github.com/tinywasm/model"
	"github.com/tinywasm/storage"
)

// Executor captures execution calls.
type Executor struct {
	ExecutedQueries []string
	ExecutedArgs    [][]any
	ReturnExecErr   error
	ReturnQueryRow  storage.Scanner
	ReturnQueryRows storage.Rows
	ReturnQueryErr  error
	ReturnCloseErr  error
}

func (m *Executor) Exec(query string, args ...any) error {
	m.ExecutedQueries = append(m.ExecutedQueries, query)
	m.ExecutedArgs = append(m.ExecutedArgs, args)
	return m.ReturnExecErr
}

func (m *Executor) QueryRow(query string, args ...any) storage.Scanner {
	m.ExecutedQueries = append(m.ExecutedQueries, query)
	m.ExecutedArgs = append(m.ExecutedArgs, args)
	if m.ReturnQueryRow == nil {
		return &Scanner{}
	}
	return m.ReturnQueryRow
}

func (m *Executor) Query(query string, args ...any) (storage.Rows, error) {
	m.ExecutedQueries = append(m.ExecutedQueries, query)
	m.ExecutedArgs = append(m.ExecutedArgs, args)
	if m.ReturnQueryRows == nil {
		return &Rows{}, m.ReturnQueryErr
	}
	return m.ReturnQueryRows, m.ReturnQueryErr
}

func (m *Executor) Close() error {
	return m.ReturnCloseErr
}

// Compiler captures the query and returns a predefined plan.
type Compiler struct {
	LastQuery  storage.Query
	LastModel  model.Model
	ReturnPlan storage.Plan
	ReturnErr  error
}

func (m *Compiler) Compile(q storage.Query, model model.Model) (storage.Plan, error) {
	m.LastQuery = q
	m.LastModel = model
	if m.ReturnPlan.Query == "" {
		m.ReturnPlan.Query = "MOCK_QUERY"
	}
	return m.ReturnPlan, m.ReturnErr
}

type Scanner struct {
	ScanErr error
}

func (m *Scanner) Scan(dest ...any) error {
	return m.ScanErr
}

type Rows struct {
	Count      int
	Current    int
	ScanErr    error
	ColumnsVal []string
	ColumnsErr error
	CloseErr   error
	ErrVal     error
}

func (m *Rows) Next() bool {
	if m.Current < m.Count {
		m.Current++
		return true
	}
	return false
}

func (m *Rows) Scan(dest ...any) error {
	return m.ScanErr
}

func (m *Rows) Columns() ([]string, error) {
	return m.ColumnsVal, m.ColumnsErr
}

func (m *Rows) Close() error {
	return m.CloseErr
}

func (m *Rows) Err() error {
	return m.ErrVal
}

// Model is a mock implementation of the model.Model interface.
type Model struct {
	Table    string
	Sch      []model.Field
	Vals     []any
	ValidErr error
}

func (m *Model) Validate(action byte) error {
	return m.ValidErr
}

func (m Model) ModelName() string     { return m.Table }
func (m Model) Schema() []model.Field { return m.Sch }
func (m Model) Pointers() []any {
	ptrs := make([]any, len(m.Vals))
	for i := range m.Vals {
		ptrs[i] = &m.Vals[i]
	}
	return ptrs
}

func (m Model) IsNil() bool                      { return false }
func (m Model) EncodeFields(w model.FieldWriter) { _ = w }
func (m Model) DecodeFields(r model.FieldReader) { _ = r }

// TxExecutor records BeginTx calls.
type TxExecutor struct {
	Executor
	Bound      *TxBoundExecutor
	BeginTxErr error
}

func (m *TxExecutor) BeginTx() (storage.TxBoundExecutor, error) {
	if m.BeginTxErr != nil {
		return nil, m.BeginTxErr
	}
	if m.Bound == nil {
		m.Bound = &TxBoundExecutor{}
	}
	return m.Bound, nil
}

type TxBoundExecutor struct {
	Executor
	CommitCalled   bool
	RollbackCalled bool
	CommitErr      error
	RollbackErr    error
}

func (m *TxBoundExecutor) Commit() error {
	m.CommitCalled = true
	return m.CommitErr
}

func (m *TxBoundExecutor) Rollback() error {
	m.RollbackCalled = true
	return m.RollbackErr
}

var (
	_ storage.Executor        = (*Executor)(nil)
	_ storage.Compiler        = (*Compiler)(nil)
	_ storage.Scanner         = (*Scanner)(nil)
	_ storage.Rows            = (*Rows)(nil)
	_ model.Model             = (*Model)(nil)
	_ storage.TxExecutor      = (*TxExecutor)(nil)
	_ storage.TxBoundExecutor = (*TxBoundExecutor)(nil)
)
