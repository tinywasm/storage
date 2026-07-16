package mem

import (
	"errors"
	"testing"

	"github.com/tinywasm/db"
	"github.com/tinywasm/model"
)

// DummyModel is a helper model implementing model.Model for testing.
type DummyModel struct {
	Id   string
	Name string
}

func (m *DummyModel) ModelName() string { return "dummy" }
func (m *DummyModel) Schema() []model.Field {
	return []model.Field{
		{Name: "id", Type: model.Text(), DB: &model.FieldDB{PK: true}},
		{Name: "name", Type: model.Text()},
	}
}
func (m *DummyModel) Pointers() []any                       { return []any{&m.Id, &m.Name} }
func (m *DummyModel) IsNil() bool                           { return m == nil }
func (m *DummyModel) EncodeFields(w model.FieldWriter)      {}
func (m *DummyModel) DecodeFields(r model.FieldReader)      {}
var _ model.Model = (*DummyModel)(nil)

func TestMemExtra(t *testing.T) {
	t.Run("BeginTx, Commit, Rollback, Close", func(t *testing.T) {
		conn := New()
		tx, err := conn.(db.TxExecutor).BeginTx()
		if err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		if err := conn.Close(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("get columns not exist", func(t *testing.T) {
		r := dbRow{dbCell{"a", 1}}
		_, ok := r.get("b")
		if ok {
			t.Error("expected ok=false")
		}
	})

	t.Run("Update/Delete on table not exist", func(t *testing.T) {
		conn := New()
		q := db.Query{Action: db.ActionUpdate, Table: "not_exist"}
		plan, _ := conn.Compile(q, &DummyModel{})
		if err := conn.Exec(plan.Query, plan.Args...); err != nil {
			t.Fatal(err)
		}

		qd := db.Query{Action: db.ActionDelete, Table: "not_exist"}
		pland, _ := conn.Compile(qd, &DummyModel{})
		if err := conn.Exec(pland.Query, pland.Args...); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("Query Columns()", func(t *testing.T) {
		conn := New()
		q := db.Query{Action: db.ActionReadAll, Table: "dummy"}
		plan, _ := conn.Compile(q, &DummyModel{})
		rows, err := conn.Query(plan.Query, plan.Args...)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		cols, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		if len(cols) != 2 || cols[0] != "id" || cols[1] != "name" {
			t.Errorf("Columns mismatch: %v", cols)
		}
	})

	t.Run("likeMatch direct exact matches and wildcards", func(t *testing.T) {
		// exact match
		if !likeMatch("abc", "abc") {
			t.Errorf("abc LIKE abc should be true")
		}
		if likeMatch("abc", "abd") {
			t.Errorf("abc LIKE abd should be false")
		}
		// wildcard % only
		if !likeMatch("any", "%") {
			t.Errorf("any LIKE %% should be true")
		}
		// prefix matches
		if !likeMatch("abcde", "abc%") {
			t.Errorf("abcde LIKE abc%% should be true")
		}
		if likeMatch("abcde", "abf%") {
			t.Errorf("abcde LIKE abf%% should be false")
		}
		// suffix matches
		if !likeMatch("abcde", "%cde") {
			t.Errorf("abcde LIKE %%cde should be true")
		}
		if likeMatch("abcde", "%cdf") {
			t.Errorf("abcde LIKE %%cdf should be false")
		}
		// middle matches
		if !likeMatch("abcde", "%bcd%") {
			t.Errorf("abcde LIKE %%bcd%% should be true")
		}
		if likeMatch("abcde", "%bcf%") {
			t.Errorf("abcde LIKE %%bcf%% should be false")
		}
		// complicated match
		if !likeMatch("abcdef", "ab%de%") {
			t.Errorf("abcdef LIKE ab%%de%% should be true")
		}
		if likeMatch("abcdef", "ab%df%") {
			t.Errorf("abcdef LIKE ab%%df%% should be false")
		}
		// prefix/suffix/find helpers edge cases
		if hasPrefixHelper("abc", "abcd") {
			t.Errorf("abc should not have prefix abcd")
		}
		if hasSuffixHelper("abc", "abcd") {
			t.Errorf("abc should not have suffix abcd")
		}
		if findHelper("abc", "") != 0 {
			t.Errorf("empty sub should find at 0")
		}
		// likeMatch with segments but no wildcards in middle or start/end
		if !likeMatch("a%b", "a%b") {
			t.Errorf("a%%b LIKE a%%b should be true")
		}
	})

	t.Run("equalAny and toStr and toFloat edge cases", func(t *testing.T) {
		if !equalAny(int(1), float64(1)) {
			t.Errorf("int(1) should equal float64(1)")
		}
		if !equalAny(float64(1), int64(1)) {
			t.Errorf("float64(1) should equal int64(1)")
		}
		if equalAny(int(1), "1") {
			t.Errorf("int(1) should not equal string '1'")
		}
		if !equalAny(true, true) {
			t.Errorf("true should equal true")
		}
		if toStr([]byte("hello")) != "hello" {
			t.Errorf("toStr of []byte failed")
		}
		if toStr(123) != "123" {
			t.Errorf("toStr of int failed")
		}
		if _, ok := toFloat("not_a_float"); ok {
			t.Errorf("toFloat of string should be false")
		}
		// toFloat for other float types
		if _, ok := toFloat(float32(1.2)); !ok {
			t.Errorf("toFloat of float32 should be true")
		}
		if _, ok := toFloat(int32(1)); !ok {
			t.Errorf("toFloat of int32 should be true")
		}
	})

	t.Run("compareAny fallback", func(t *testing.T) {
		if compareAny("a", "b") >= 0 {
			t.Errorf("a < b")
		}
		if compareAny("b", "a") <= 0 {
			t.Errorf("b > a")
		}
		if compareAny("a", "a") != 0 {
			t.Errorf("a == a")
		}
		if compareAny(int(1), float64(2)) >= 0 {
			t.Errorf("1 < 2")
		}
		if compareAny(float64(2), int(1)) <= 0 {
			t.Errorf("2 > 1")
		}
		if compareAny(int(1), int64(1)) != 0 {
			t.Errorf("1 == 1")
		}
	})

	t.Run("inSlice types", func(t *testing.T) {
		if inSlice(nil, nil) {
			t.Errorf("nil in nil")
		}
		if !inSlice("a", []string{"a", "b"}) {
			t.Errorf("a in []string")
		}
		if !inSlice(123, []int{123, 456}) {
			t.Errorf("123 in []int")
		}
		if !inSlice(int64(123), []int64{123, 456}) {
			t.Errorf("123 in []int64")
		}
		if inSlice("c", []string{"a", "b"}) {
			t.Errorf("c in []string")
		}
		if inSlice(789, []int{123, 456}) {
			t.Errorf("789 in []int")
		}
		if inSlice("c", []any{"a", "b"}) {
			t.Errorf("c in []any")
		}
		if !inSlice("a", []any{"a", "b"}) {
			t.Errorf("a in []any")
		}
	})

	t.Run("applyOffsetLimit edge cases", func(t *testing.T) {
		rows := []dbRow{
			{dbCell{"id", "1"}},
			{dbCell{"id", "2"}},
		}
		res := applyOffsetLimit(rows, 5, 1)
		if len(res) != 0 {
			t.Errorf("expected empty rows, got %v", res)
		}
	})

	t.Run("Scan with short dest", func(t *testing.T) {
		conn := New()
		// seed
		q := db.Query{
			Action:  db.ActionCreate,
			Table:   "dummy",
			Columns: []string{"id", "name"},
			Values:  []any{"d1", "dummy_name"},
		}
		plan, _ := conn.Compile(q, &DummyModel{})
		conn.Exec(plan.Query, plan.Args...)

		qr := db.Query{Action: db.ActionReadOne, Table: "dummy"}
		planr, _ := conn.Compile(qr, &DummyModel{})
		scanner := conn.QueryRow(planr.Query, planr.Args...)

		var id string
		if err := scanner.Scan(&id); err != nil {
			t.Fatal(err)
		}
		if id != "d1" {
			t.Errorf("expected d1, got %s", id)
		}
	})

	t.Run("Scan with unsupported type", func(t *testing.T) {
		conn := New()
		// seed
		q := db.Query{
			Action:  db.ActionCreate,
			Table:   "dummy",
			Columns: []string{"id", "name"},
			Values:  []any{"d1", "dummy_name"},
		}
		plan, _ := conn.Compile(q, &DummyModel{})
		conn.Exec(plan.Query, plan.Args...)

		qr := db.Query{Action: db.ActionReadOne, Table: "dummy"}
		planr, _ := conn.Compile(qr, &DummyModel{})
		scanner := conn.QueryRow(planr.Query, planr.Args...)

		var id complex128
		err := scanner.Scan(&id)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("scan missing column in row gets ignored/nil", func(t *testing.T) {
		// we scan a row that only has "id" but schema has "id", "name".
		// we pass dest for "name", but it should remain unchanged/nil.
		row := dbRow{dbCell{"id", "d1"}}
		schema := []model.Field{
			{Name: "id", Type: model.Text()},
			{Name: "name", Type: model.Text()},
		}
		var id string
		var name string = "initial"
		err := scanInto(row, schema, []any{&id, &name})
		if err != nil {
			t.Fatal(err)
		}
		if id != "d1" {
			t.Errorf("id should be d1, got %q", id)
		}
		if name != "initial" {
			t.Errorf("name should not have been updated, got %q", name)
		}
	})

	t.Run("Scanner error return", func(t *testing.T) {
		s := memScanner{err: errors.New("some error")}
		var id string
		if err := s.Scan(&id); err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}
