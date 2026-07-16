package mock

import (
	"errors"
	"testing"

	"github.com/tinywasm/model"
	"github.com/tinywasm/storage"
)

func TestMockExecutorAndCompiler(t *testing.T) {
	t.Run("Executor basics default", func(t *testing.T) {
		exec := &Executor{}
		err := exec.Exec("INSERT INTO foo", 1, 2)
		if err != nil {
			t.Fatal(err)
		}
		if len(exec.ExecutedQueries) != 1 || exec.ExecutedQueries[0] != "INSERT INTO foo" {
			t.Errorf("unexpected queries: %v", exec.ExecutedQueries)
		}
		if len(exec.ExecutedArgs) != 1 || len(exec.ExecutedArgs[0]) != 2 || exec.ExecutedArgs[0][0] != 1 {
			t.Errorf("unexpected args: %v", exec.ExecutedArgs)
		}

		sc := exec.QueryRow("SELECT foo", 1)
		if sc == nil {
			t.Fatal("expected scanner")
		}
		if err := sc.Scan(); err != nil {
			t.Fatal(err)
		}

		rows, err := exec.Query("SELECT foo", 1)
		if err != nil {
			t.Fatal(err)
		}
		if rows == nil {
			t.Fatal("expected rows")
		}
		if rows.Next() {
			t.Error("expected no rows next")
		}

		if err := exec.Close(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("Executor errors injection", func(t *testing.T) {
		exec := &Executor{
			ReturnExecErr:   errors.New("exec error"),
			ReturnQueryErr:  errors.New("query error"),
			ReturnCloseErr:  errors.New("close error"),
			ReturnQueryRow:  &Scanner{ScanErr: errors.New("scan error")},
			ReturnQueryRows: &Rows{ScanErr: errors.New("rows scan error"), ColumnsVal: []string{"a"}, ColumnsErr: errors.New("cols error"), CloseErr: errors.New("close err"), ErrVal: errors.New("err val")},
		}

		if err := exec.Exec("INSERT"); err == nil || err.Error() != "exec error" {
			t.Errorf("expected exec error, got %v", err)
		}

		sc := exec.QueryRow("SELECT")
		if err := sc.Scan(); err == nil || err.Error() != "scan error" {
			t.Errorf("expected scan error, got %v", err)
		}

		rows, err := exec.Query("SELECT")
		if err == nil || err.Error() != "query error" {
			t.Errorf("expected query error, got %v", err)
		}
		if rows != nil {
			if err := rows.Scan(); err == nil || err.Error() != "rows scan error" {
				t.Errorf("expected rows scan error, got %v", err)
			}
			if _, err := rows.Columns(); err == nil || err.Error() != "cols error" {
				t.Errorf("expected cols error, got %v", err)
			}
			if err := rows.Close(); err == nil || err.Error() != "close err" {
				t.Errorf("expected close error, got %v", err)
			}
			if err := rows.Err(); err == nil || err.Error() != "err val" {
				t.Errorf("expected err val, got %v", err)
			}
		}

		if err := exec.Close(); err == nil || err.Error() != "close error" {
			t.Errorf("expected close error, got %v", err)
		}
	})

	t.Run("Compiler basics", func(t *testing.T) {
		comp := &Compiler{}
		q := storage.Query{Table: "foo"}
		m := &Model{}
		plan, err := comp.Compile(q, m)
		if err != nil {
			t.Fatal(err)
		}
		if plan.Query != "MOCK_QUERY" {
			t.Errorf("expected MOCK_QUERY, got %s", plan.Query)
		}
		if comp.LastQuery.Table != "foo" {
			t.Errorf("expected foo table, got %s", comp.LastQuery.Table)
		}

		comp.ReturnPlan = storage.Plan{Query: "CUSTOM_QUERY"}
		comp.ReturnErr = errors.New("compile error")
		_, err = comp.Compile(q, m)
		if err == nil || err.Error() != "compile error" {
			t.Errorf("expected compile error, got %v", err)
		}
	})

	t.Run("Model basics", func(t *testing.T) {
		m := Model{
			Table: "my_table",
			Sch:   []model.Field{{Name: "id", Type: model.Text()}},
			Vals:  []any{"v1"},
		}
		if m.ModelName() != "my_table" {
			t.Errorf("model name mismatch: %s", m.ModelName())
		}
		if len(m.Schema()) != 1 || m.Schema()[0].Name != "id" {
			t.Errorf("schema mismatch: %v", m.Schema())
		}
		ptrs := m.Pointers()
		if len(ptrs) != 1 {
			t.Errorf("pointers mismatch: %v", ptrs)
		}
		if m.IsNil() {
			t.Error("expected not nil")
		}
		m.EncodeFields(nil)
		m.DecodeFields(nil)
		if err := m.Validate('c'); err != nil {
			t.Fatal(err)
		}
		m.ValidErr = errors.New("invalid")
		if err := m.Validate('c'); err == nil {
			t.Error("expected validation error")
		}
	})

	t.Run("TxExecutor basics", func(t *testing.T) {
		txExec := &TxExecutor{}
		tx, err := txExec.BeginTx()
		if err != nil {
			t.Fatal(err)
		}
		if tx == nil {
			t.Fatal("expected tx")
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		if !txExec.Bound.CommitCalled || !txExec.Bound.RollbackCalled {
			t.Error("expected commit/rollback to be called")
		}

		txExec.BeginTxErr = errors.New("begin error")
		_, err = txExec.BeginTx()
		if err == nil || err.Error() != "begin error" {
			t.Errorf("expected begin error, got %v", err)
		}

		bound := &TxBoundExecutor{
			CommitErr:   errors.New("commit error"),
			RollbackErr: errors.New("rollback error"),
		}
		if err := bound.Commit(); err == nil || err.Error() != "commit error" {
			t.Errorf("expected commit error, got %v", err)
		}
		if err := bound.Rollback(); err == nil || err.Error() != "rollback error" {
			t.Errorf("expected rollback error, got %v", err)
		}
	})

	t.Run("Rows count test", func(t *testing.T) {
		rows := &Rows{Count: 2}
		if !rows.Next() {
			t.Error("expected first next to be true")
		}
		if !rows.Next() {
			t.Error("expected second next to be true")
		}
		if rows.Next() {
			t.Error("expected third next to be false")
		}
	})
}
