package tests

import (
	"errors"
	"testing"

	"github.com/tinywasm/model"
	"github.com/tinywasm/storage"
	"github.com/tinywasm/storage/mem"
)

// ExtraDummy is a helper model for testing mem extras.
type ExtraDummy struct {
	Id     string
	Name   string
	Qty    int64
	Active bool
}

var ExtraDummyModel = model.Definition{
	Name: "extra_dummy",
	Fields: model.Fields{
		{Name: "id", Type: model.Text(), DB: &model.FieldDB{PK: true}},
		{Name: "name", Type: model.Text()},
		{Name: "qty", Type: model.Int()},
		{Name: "active", Type: model.Bool()},
	},
}

func (m *ExtraDummy) ModelName() string             { return ExtraDummyModel.Name }
func (m *ExtraDummy) Schema() []model.Field         { return ExtraDummyModel.Fields }
func (m *ExtraDummy) Pointers() []any               { return []any{&m.Id, &m.Name, &m.Qty, &m.Active} }
func (m *ExtraDummy) IsNil() bool                   { return m == nil }
func (m *ExtraDummy) EncodeFields(w model.FieldWriter) {}
func (m *ExtraDummy) DecodeFields(r model.FieldReader) {}

var _ model.Model = (*ExtraDummy)(nil)

func TestMemExtra(t *testing.T) {
	t.Run("BeginTx, Commit, Rollback, Close", func(t *testing.T) {
		conn := mem.New()
		tx, err := conn.(storage.TxExecutor).BeginTx()
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

	t.Run("Update/Delete on table not exist", func(t *testing.T) {
		conn := mem.New()
		qUpdate := storage.Query{
			Action: storage.ActionUpdate,
			Table:  "not_exist",
		}
		plan, _ := conn.Compile(qUpdate, &ExtraDummy{})
		if err := conn.Exec(plan.Query, plan.Args...); err != nil {
			t.Fatal(err)
		}

		qDelete := storage.Query{
			Action: storage.ActionDelete,
			Table:  "not_exist",
		}
		pland, _ := conn.Compile(qDelete, &ExtraDummy{})
		if err := conn.Exec(pland.Query, pland.Args...); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("Query Columns()", func(t *testing.T) {
		conn := mem.New()
		q := storage.Query{Action: storage.ActionReadAll, Table: "extra_dummy"}
		plan, _ := conn.Compile(q, &ExtraDummy{})
		rows, err := conn.Query(plan.Query, plan.Args...)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		cols, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		if len(cols) != 4 || cols[0] != "id" || cols[1] != "name" {
			t.Errorf("Columns mismatch: %v", cols)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("likeMatch wildcards", func(t *testing.T) {
		conn := mem.New()
		seed := []ExtraDummy{
			{Id: "1", Name: "abc"},
			{Id: "2", Name: "abcdef"},
			{Id: "3", Name: "a%b"},
		}
		for _, item := range seed {
			q := storage.Query{
				Action:  storage.ActionCreate,
				Table:   item.ModelName(),
				Columns: []string{"id", "name", "qty", "active"},
				Values:  []any{item.Id, item.Name, item.Qty, item.Active},
			}
			plan, _ := conn.Compile(q, &item)
			conn.Exec(plan.Query, plan.Args...)
		}

		cases := []struct {
			pattern string
			wantIds []string
		}{
			{"abc", []string{"1"}},
			{"%", []string{"1", "2", "3"}},
			{"abc%", []string{"1", "2"}},
			{"%def", []string{"2"}},
			{"%bcd%", []string{"2"}},
			{"ab%de%", []string{"2"}},
			{"a%b", []string{"3"}},
		}

		for _, tc := range cases {
			q := storage.Query{
				Action:     storage.ActionReadAll,
				Table:      "extra_dummy",
				Conditions: []storage.Condition{storage.Like("name", tc.pattern)},
			}
			plan, _ := conn.Compile(q, &ExtraDummy{})
			rows, err := conn.Query(plan.Query, plan.Args...)
			if err != nil {
				t.Fatalf("pattern %q error: %v", tc.pattern, err)
			}
			var gotIds []string
			for rows.Next() {
				var got ExtraDummy
				rows.Scan(got.Pointers()...)
				gotIds = append(gotIds, got.Id)
			}
			rows.Close()

			if len(gotIds) != len(tc.wantIds) {
				t.Errorf("pattern %q: expected %v, got %v", tc.pattern, tc.wantIds, gotIds)
			}
		}
	})

	t.Run("equalAny toStr toFloat compareAny", func(t *testing.T) {
		conn := mem.New()
		seed := []ExtraDummy{
			{Id: "1", Name: "val1", Qty: 10, Active: true},
			{Id: "2", Name: "val2", Qty: 20, Active: false},
		}
		for _, item := range seed {
			q := storage.Query{
				Action:  storage.ActionCreate,
				Table:   item.ModelName(),
				Columns: []string{"id", "name", "qty", "active"},
				Values:  []any{item.Id, item.Name, item.Qty, item.Active},
			}
			plan, _ := conn.Compile(q, &item)
			conn.Exec(plan.Query, plan.Args...)
		}

		// Comparison queries to cover comparison float/int edge cases
		q := storage.Query{
			Action:     storage.ActionReadAll,
			Table:      "extra_dummy",
			Conditions: []storage.Condition{storage.Gt("qty", 15)},
		}
		plan, _ := conn.Compile(q, &ExtraDummy{})
		rows, err := conn.Query(plan.Query, plan.Args...)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var got []ExtraDummy
		for rows.Next() {
			var d ExtraDummy
			rows.Scan(d.Pointers()...)
			got = append(got, d)
		}
		if len(got) != 1 || got[0].Id != "2" {
			t.Errorf("expected only Id 2, got %+v", got)
		}
	})

	t.Run("inSlice types", func(t *testing.T) {
		conn := mem.New()
		seed := []ExtraDummy{
			{Id: "1", Name: "a", Qty: 10},
			{Id: "2", Name: "b", Qty: 20},
		}
		for _, item := range seed {
			q := storage.Query{
				Action:  storage.ActionCreate,
				Table:   item.ModelName(),
				Columns: []string{"id", "name", "qty", "active"},
				Values:  []any{item.Id, item.Name, item.Qty, item.Active},
			}
			plan, _ := conn.Compile(q, &item)
			conn.Exec(plan.Query, plan.Args...)
		}

		cases := []struct {
			cond storage.Condition
			want []string
		}{
			{storage.In("id", []string{"1", "3"}), []string{"1"}},
			{storage.In("qty", []int{10, 30}), []string{"1"}},
			{storage.In("qty", []int64{20}), []string{"2"}},
			{storage.In("name", []any{"b", "c"}), []string{"2"}},
		}

		for i, tc := range cases {
			q := storage.Query{
				Action:     storage.ActionReadAll,
				Table:      "extra_dummy",
				Conditions: []storage.Condition{tc.cond},
			}
			plan, _ := conn.Compile(q, &ExtraDummy{})
			rows, _ := conn.Query(plan.Query, plan.Args...)
			var got []string
			for rows.Next() {
				var d ExtraDummy
				rows.Scan(d.Pointers()...)
				got = append(got, d.Id)
			}
			rows.Close()
			if len(got) != len(tc.want) || (len(got) > 0 && got[0] != tc.want[0]) {
				t.Errorf("case %d: expected %v, got %v", i, tc.want, got)
			}
		}
	})

	t.Run("applyOffsetLimit edge cases", func(t *testing.T) {
		conn := mem.New()
		seed := []ExtraDummy{
			{Id: "1", Name: "a"},
			{Id: "2", Name: "b"},
		}
		for _, item := range seed {
			q := storage.Query{
				Action:  storage.ActionCreate,
				Table:   item.ModelName(),
				Columns: []string{"id", "name", "qty", "active"},
				Values:  []any{item.Id, item.Name, item.Qty, item.Active},
			}
			plan, _ := conn.Compile(q, &item)
			conn.Exec(plan.Query, plan.Args...)
		}

		q := storage.Query{
			Action: storage.ActionReadAll,
			Table:  "extra_dummy",
			Offset: 5,
			Limit:  1,
		}
		plan, _ := conn.Compile(q, &ExtraDummy{})
		rows, err := conn.Query(plan.Query, plan.Args...)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		if rows.Next() {
			t.Error("expected no rows since offset is larger than row count")
		}
	})

	t.Run("Scan with short dest or unsupported type", func(t *testing.T) {
		conn := mem.New()
		item := ExtraDummy{Id: "1", Name: "dummy_name"}
		q := storage.Query{
			Action:  storage.ActionCreate,
			Table:   item.ModelName(),
			Columns: []string{"id", "name", "qty", "active"},
			Values:  []any{item.Id, item.Name, item.Qty, item.Active},
		}
		plan, _ := conn.Compile(q, &item)
		conn.Exec(plan.Query, plan.Args...)

		qr := storage.Query{Action: storage.ActionReadOne, Table: "extra_dummy"}
		planr, _ := conn.Compile(qr, &ExtraDummy{})
		scanner := conn.QueryRow(planr.Query, planr.Args...)

		var id string
		if err := scanner.Scan(&id); err != nil {
			t.Fatal(err)
		}
		if id != "1" {
			t.Errorf("expected 1, got %s", id)
		}

		// scan unsupported type
		var unsupported complex128
		scanner2 := conn.QueryRow(planr.Query, planr.Args...)
		err := scanner2.Scan(&unsupported)
		if err == nil {
			t.Fatal("expected unsupported type error, got nil")
		}
	})

	t.Run("scan missing column in row gets ignored/nil", func(t *testing.T) {
		conn := mem.New()
		// we insert a row that doesn't define 'qty' or 'active', only 'id' and 'name'.
		item := ExtraDummy{Id: "1", Name: "dummy_name"}
		q := storage.Query{
			Action:  storage.ActionCreate,
			Table:   item.ModelName(),
			Columns: []string{"id", "name"},
			Values:  []any{item.Id, item.Name},
		}
		plan, _ := conn.Compile(q, &item)
		conn.Exec(plan.Query, plan.Args...)

		qr := storage.Query{Action: storage.ActionReadOne, Table: "extra_dummy"}
		planr, _ := conn.Compile(qr, &ExtraDummy{})
		scanner := conn.QueryRow(planr.Query, planr.Args...)

		var gotId string
		var gotName string
		var gotQty int64 = 999
		var gotActive bool = true

		if err := scanner.Scan(&gotId, &gotName, &gotQty, &gotActive); err != nil {
			t.Fatal(err)
		}
		if gotId != "1" || gotName != "dummy_name" {
			t.Errorf("expected 1 and dummy_name, got %s and %s", gotId, gotName)
		}
		if gotQty != 999 {
			t.Errorf("expected gotQty to remain 999, got %d", gotQty)
		}
		if !gotActive {
			t.Errorf("expected gotActive to remain true, got false")
		}
	})

	t.Run("Scanner error return", func(t *testing.T) {
		conn := mem.New()
		qr := storage.Query{
			Action:     storage.ActionReadOne,
			Table:      "extra_dummy",
			Conditions: []storage.Condition{storage.Eq("id", "nonexistent")},
		}
		plan, _ := conn.Compile(qr, &ExtraDummy{})
		scanner := conn.QueryRow(plan.Query, plan.Args...)

		var id string
		err := scanner.Scan(&id)
		if err == nil || !errors.Is(err, storage.ErrNoRows) {
			t.Errorf("expected storage.ErrNoRows, got %v", err)
		}
	})
}
