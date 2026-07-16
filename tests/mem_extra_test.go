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
			{"ab%de%xy", nil}, // suffix mismatch
			{"xy%de%gh", nil}, // prefix mismatch
			{"ab%xy%ef", nil}, // findHelper mismatch
			{"abcdef%", []string{"2"}}, // matches ID 2, triggers len(s) < len(prefix) on ID 1
			{"%abcdefg", nil}, // hasSuffixHelper len check (len(s) < len(suffix))
			{"ab%%cd", nil},   // findHelper with empty sub
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

		// Gt with float64
		q := storage.Query{
			Action:     storage.ActionReadAll,
			Table:      "extra_dummy",
			Conditions: []storage.Condition{storage.Gt("qty", float64(15.5))},
		}
		plan, _ := conn.Compile(q, &ExtraDummy{})
		rows, err := conn.Query(plan.Query, plan.Args...)
		if err != nil {
			t.Fatal(err)
		}
		var got []ExtraDummy
		for rows.Next() {
			var d ExtraDummy
			rows.Scan(d.Pointers()...)
			got = append(got, d)
		}
		rows.Close()
		if len(got) != 1 || got[0].Id != "2" {
			t.Errorf("expected only Id 2, got %+v", got)
		}

		// Gt with float32
		q2 := storage.Query{
			Action:     storage.ActionReadAll,
			Table:      "extra_dummy",
			Conditions: []storage.Condition{storage.Gt("qty", float32(15.5))},
		}
		plan2, _ := conn.Compile(q2, &ExtraDummy{})
		rows2, _ := conn.Query(plan2.Query, plan2.Args...)
		got = nil
		for rows2.Next() {
			var d ExtraDummy
			rows2.Scan(d.Pointers()...)
			got = append(got, d)
		}
		rows2.Close()
		if len(got) != 1 || got[0].Id != "2" {
			t.Errorf("expected only Id 2, got %+v", got)
		}

		// Gt with int32
		q3 := storage.Query{
			Action:     storage.ActionReadAll,
			Table:      "extra_dummy",
			Conditions: []storage.Condition{storage.Gt("qty", int32(15))},
		}
		plan3, _ := conn.Compile(q3, &ExtraDummy{})
		rows3, _ := conn.Query(plan3.Query, plan3.Args...)
		got = nil
		for rows3.Next() {
			var d ExtraDummy
			rows3.Scan(d.Pointers()...)
			got = append(got, d)
		}
		rows3.Close()
		if len(got) != 1 || got[0].Id != "2" {
			t.Errorf("expected only Id 2, got %+v", got)
		}

		// Eq with []byte (testing toStr []byte branch)
		q4 := storage.Query{
			Action:     storage.ActionReadAll,
			Table:      "extra_dummy",
			Conditions: []storage.Condition{storage.Eq("name", []byte("val1"))},
		}
		plan4, _ := conn.Compile(q4, &ExtraDummy{})
		rows4, _ := conn.Query(plan4.Query, plan4.Args...)
		got = nil
		for rows4.Next() {
			var d ExtraDummy
			rows4.Scan(d.Pointers()...)
			got = append(got, d)
		}
		rows4.Close()
		if len(got) != 1 || got[0].Id != "1" {
			t.Errorf("expected only Id 1, got %+v", got)
		}

		// Gt with strings (testing compareAny with string)
		q5 := storage.Query{
			Action:     storage.ActionReadAll,
			Table:      "extra_dummy",
			Conditions: []storage.Condition{storage.Gt("name", "val1")},
		}
		plan5, _ := conn.Compile(q5, &ExtraDummy{})
		rows5, _ := conn.Query(plan5.Query, plan5.Args...)
		got = nil
		for rows5.Next() {
			var d ExtraDummy
			rows5.Scan(d.Pointers()...)
			got = append(got, d)
		}
		rows5.Close()
		if len(got) != 1 || got[0].Id != "2" {
			t.Errorf("expected only Id 2, got %+v", got)
		}

		// equalAny fallback check comparing qty (int64 -> float) against a boolean (non-float)
		q6 := storage.Query{
			Action:     storage.ActionReadAll,
			Table:      "extra_dummy",
			Conditions: []storage.Condition{storage.Eq("qty", true)},
		}
		plan6, _ := conn.Compile(q6, &ExtraDummy{})
		rows6, _ := conn.Query(plan6.Query, plan6.Args...)
		got = nil
		for rows6.Next() {
			var d ExtraDummy
			rows6.Scan(d.Pointers()...)
			got = append(got, d)
		}
		rows6.Close()
		if len(got) != 0 {
			t.Errorf("expected 0 matches, got %+v", got)
		}

		// Gt with non-float fallback toStr default (comparing string "name" against "int" qty value 15)
		q7 := storage.Query{
			Action:     storage.ActionReadAll,
			Table:      "extra_dummy",
			Conditions: []storage.Condition{storage.Gt("name", int(15))},
		}
		plan7, _ := conn.Compile(q7, &ExtraDummy{})
		rows7, _ := conn.Query(plan7.Query, plan7.Args...)
		got = nil
		for rows7.Next() {
			var d ExtraDummy
			rows7.Scan(d.Pointers()...)
			got = append(got, d)
		}
		rows7.Close()
		if len(got) != 2 {
			t.Errorf("expected both matches (since 'val1' > '15'), got %+v", got)
		}

		// Gt with equal float (comparing 'qty' against exactly 10, hits default: return 0 in float comparison inside compareAny)
		q8 := storage.Query{
			Action:     storage.ActionReadAll,
			Table:      "extra_dummy",
			Conditions: []storage.Condition{storage.Gt("qty", 10)},
		}
		plan8, _ := conn.Compile(q8, &ExtraDummy{})
		rows8, _ := conn.Query(plan8.Query, plan8.Args...)
		got = nil
		for rows8.Next() {
			var d ExtraDummy
			rows8.Scan(d.Pointers()...)
			got = append(got, d)
		}
		rows8.Close()
		if len(got) != 1 || got[0].Id != "2" {
			t.Errorf("expected only ID 2 matching, got %+v", got)
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
