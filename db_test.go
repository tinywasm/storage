package db_test

import (
	"errors"
	"testing"

	"github.com/tinywasm/db"
)

func TestConditions(t *testing.T) {
	cases := []struct {
		name     string
		cond     db.Condition
		field    string
		operator string
		value    any
		logic    string
	}{
		{"Eq", db.Eq("foo", "bar"), "foo", "=", "bar", "AND"},
		{"Neq", db.Neq("foo", "bar"), "foo", "!=", "bar", "AND"},
		{"Gt", db.Gt("foo", 123), "foo", ">", 123, "AND"},
		{"Gte", db.Gte("foo", 123), "foo", ">=", 123, "AND"},
		{"Lt", db.Lt("foo", 123), "foo", "<", 123, "AND"},
		{"Lte", db.Lte("foo", 123), "foo", "<=", 123, "AND"},
		{"Like", db.Like("foo", "%bar"), "foo", "LIKE", "%bar", "AND"},
		{"In", db.In("foo", "bar"), "foo", "IN", "bar", "AND"}, // Changed []any to a string to avoid panic on uncomparable types during test loop
		{"IsNotNull", db.IsNotNull("foo"), "foo", "IS NOT NULL", nil, "AND"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.cond.Field() != tc.field {
				t.Errorf("expected Field %q, got %q", tc.field, tc.cond.Field())
			}
			if tc.cond.Operator() != tc.operator {
				t.Errorf("expected Operator %q, got %q", tc.operator, tc.cond.Operator())
			}
			if tc.cond.Value() != tc.value {
				t.Errorf("expected Value %v, got %v", tc.value, tc.cond.Value())
			}
			if tc.cond.Logic() != tc.logic {
				t.Errorf("expected Logic %q, got %q", tc.logic, tc.cond.Logic())
			}
		})
	}

	t.Run("Or", func(t *testing.T) {
		c := db.Or(db.Eq("foo", "bar"))
		if c.Field() != "foo" || c.Operator() != "=" || c.Value() != "bar" || c.Logic() != "OR" {
			t.Errorf("Or wrapper behaved incorrectly: %+v", c)
		}
	})
}

func TestOrder(t *testing.T) {
	asc := db.Asc("foo")
	if asc.Column() != "foo" || asc.Dir() != "ASC" {
		t.Errorf("Asc failed: %+v", asc)
	}

	desc := db.Desc("bar")
	if desc.Column() != "bar" || desc.Dir() != "DESC" {
		t.Errorf("Desc failed: %+v", desc)
	}
}

func TestScanAny(t *testing.T) {
	t.Run("string", func(t *testing.T) {
		var s string
		if err := db.ScanAny("hello", &s); err != nil {
			t.Fatal(err)
		}
		if s != "hello" {
			t.Errorf("expected hello, got %q", s)
		}
	})

	t.Run("int from float64", func(t *testing.T) {
		var i int
		if err := db.ScanAny(float64(42), &i); err != nil {
			t.Fatal(err)
		}
		if i != 42 {
			t.Errorf("expected 42, got %d", i)
		}
	})

	t.Run("int from int64", func(t *testing.T) {
		var i int
		if err := db.ScanAny(int64(42), &i); err != nil {
			t.Fatal(err)
		}
		if i != 42 {
			t.Errorf("expected 42, got %d", i)
		}
	})

	t.Run("int64 from float64", func(t *testing.T) {
		var i int64
		if err := db.ScanAny(float64(42), &i); err != nil {
			t.Fatal(err)
		}
		if i != 42 {
			t.Errorf("expected 42, got %d", i)
		}
	})

	t.Run("int64 from int64", func(t *testing.T) {
		var i int64
		if err := db.ScanAny(int64(42), &i); err != nil {
			t.Fatal(err)
		}
		if i != 42 {
			t.Errorf("expected 42, got %d", i)
		}
	})

	t.Run("float64", func(t *testing.T) {
		var f float64
		if err := db.ScanAny(1.23, &f); err != nil {
			t.Fatal(err)
		}
		if f != 1.23 {
			t.Errorf("expected 1.23, got %f", f)
		}
	})

	t.Run("bool", func(t *testing.T) {
		var b bool
		if err := db.ScanAny(true, &b); err != nil {
			t.Fatal(err)
		}
		if !b {
			t.Errorf("expected true, got false")
		}
	})

	t.Run("bytes from bytes", func(t *testing.T) {
		var b []byte
		if err := db.ScanAny([]byte("hello"), &b); err != nil {
			t.Fatal(err)
		}
		if string(b) != "hello" {
			t.Errorf("expected hello, got %s", b)
		}
	})

	t.Run("bytes from string", func(t *testing.T) {
		var b []byte
		if err := db.ScanAny("hello", &b); err != nil {
			t.Fatal(err)
		}
		if string(b) != "hello" {
			t.Errorf("expected hello, got %s", b)
		}
	})

	t.Run("any", func(t *testing.T) {
		var v any
		if err := db.ScanAny("hello", &v); err != nil {
			t.Fatal(err)
		}
		if v != "hello" {
			t.Errorf("expected hello, got %v", v)
		}
	})

	t.Run("unsupported dest", func(t *testing.T) {
		var x complex128
		err := db.ScanAny("hello", &x)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestErrors(t *testing.T) {
	if !errors.Is(db.ErrNoRows, db.ErrNoRows) {
		t.Error("ErrNoRows doesn't match itself")
	}
}
