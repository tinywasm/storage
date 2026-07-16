---
PLAN: "feat: tinywasm/db — puerto de almacenamiento (contrato + conformance + mem + mock)"
TAG: v0.0.1
---

> **Prerequisito (entorno del agente):**
> ```bash
> go install github.com/tinywasm/devflow/cmd/gotest@latest
> ```
> Tests SIEMPRE con `gotest` (no `go test`). Publica SIEMPRE con `gopush 'mensaje'` (no
> `git commit`/`git push`). El tag lo pone `gopush`.

Eres un agente **sin contexto previo** y **solo tienes este repo** (`github.com/tinywasm/db`). Todo el
contrato y el código exacto van inline — no necesitas leer `tinywasm/orm` para ejecutar este plan.

---

## 0. Qué es este repo y por qué existe

`tinywasm/orm` mezclaba dos responsabilidades con dos audiencias distintas: **el contrato** que un
backend de almacenamiento (`postgres`, `sqlt`, `indexdb`) debe implementar, y **la API ergonómica**
(query builder `Where/OrderBy/ReadAll`) que una app/módulo hoja usa. Que ambas vivieran en `orm`
obligaba a todo backend a depender del ORM completo solo para cumplir unas interfaces — arquitectura
invertida: un driver de base de datos no debería depender de un ORM.

Este repo, `tinywasm/db`, es **el puerto** (en el sentido de arquitectura hexagonal): las interfaces,
los tipos de valor, el contrato ejecutable (`conformance`) y una implementación de referencia en
memoria (`mem`) + dobles de test (`mock`). Es el precedente exacto de `database/sql/driver` en la
stdlib de Go — `database/sql` (`sql.DB`, ergonomía) es opcional sobre `database/sql/driver` (el
contrato); nunca al revés. `tinywasm/orm` pasará a ser el equivalente de `sql.DB`: una capa ergonómica
**opcional** encima de `db` (ver razonamiento completo en
[`app-releases/docs/DB_PORT_PROPOSAL.md`](https://github.com/tinywasm/app-releases/blob/main/docs/DB_PORT_PROPOSAL.md)
§6.8 — por qué el ORM no es parte del contrato).

**Alcance de este plan: SOLO `tinywasm/db`.** No toques `tinywasm/orm`, `tinywasm/ddl`, ni ningún
backend (`postgres`/`sqlt`/`indexdb`). Migrarlos a depender de `db` es trabajo de fases posteriores,
despachadas por separado. Este repo debe quedar **completo y publicable por sí solo**.

## 1. Arquitectura y regla de dependencia

```
tinywasm/model   (Field, Definition, FieldWriter/Reader, ReadValues)                    [fmt]
      │
      ▼
tinywasm/db      — ESTE REPO. El puerto:
      │             · raíz: Executor, Compiler, Conn, Scanner, Rows, TxExecutor,
      │               TxBoundExecutor, Query, Action, Order, Condition, Plan,
      │               ErrNoRows, ScanAny
      │             · db/conformance  — contrato DML ejecutable (Run(t, Factory))
      │             · db/mem          — backend en memoria de referencia (mem.New() db.Conn)
      │             · db/mock         — recorders (dobles que capturan la Query)
      │
      └── (consumido después por orm, ddl, postgres, sqlt, indexdb — NO en este plan)
```

- `db` depende **solo** de `tinywasm/model` y `tinywasm/fmt`. No importa `orm`, `ddl`, ni ningún driver.
- `db` es **isomórfico**: compila para `GOOS=js GOARCH=wasm` y para TinyGo sin excepción — indexdb (un
  backend WASM) implementará `db.Conn` directamente. Ver `AGENTS.md` para las restricciones exactas
  (sin `map`, sin `database/sql`, sin `reflect`).
- **Sin Go `map` en ningún archivo.** TinyGo infla el binario wasm con el runtime de hashmap. Todo
  lookup en este repo se hace con slices escaneados linealmente. El código inline de este plan ya está
  escrito así — respétalo, no "optimices" reintroduciendo un `map[K]V`.

## 2. Qué NO se incluye (decisiones ya tomadas, no las reabras)

- **Sin DDL.** `CreateTable`/`Sync`/`Action` DDL viven en `tinywasm/ddl`, que consumirá `db.Conn` +
  `db.Compiler` (no `orm`) en una fase posterior. Este repo no sabe nada de esquema.
  - **Nota para cuando se despache esa fase (no ahora):** el `Sync` de `ddl` necesita compilar un
    `SELECT` (el safe-drop, una lectura DML real) — con `db` existiendo, `ddl.New` tomará un
    `db.Compiler` en vez del `orm.Compiler` que el plan viejo de `ddl` pedía. Es una simplificación,
    no una obligación de este repo.
- **Sin query builder ni `DB` ergonómico.** `Where/OrderBy/Limit/ReadOne/ReadAll/Create/Update/Delete`
  son responsabilidad de `orm` (capa opcional encima de este contrato). Si sientes la tentación de
  añadir un helper "para que sea más cómodo usar `db` directamente", no lo hagas — es exactamente la
  responsabilidad que este split separa. Ver DB_PORT_PROPOSAL.md §6.3/§6.4/§6.8.
- **Sin registro DSN (`Open`/`Register`).** El registro por string que hoy tiene `orm/open.go` se
  **elimina** en el ecosistema (no se traslada aquí): un lookup por string que falla en runtime viola
  el harness (fail at compile time). Los backends expondrán constructores tipados
  (`sqlt.Open(dsn) (db.Conn, error)`); el ensamblaje con la capa ergonómica lo hace el consumidor
  (`orm.New(conn)`). Eso también es una fase posterior — aquí no hay nada que hacer al respecto, solo
  no reintroduzcas el registro.
- **Sin `ErrNotFound`.** Ese es un concepto de la API ergonómica ("no hubo fila para tu `ReadOne`").
  `db` solo conoce `ErrNoRows` (el sentinela crudo que un `Conn` debe devolver cuando no hay filas). La
  traducción a `ErrNotFound` es trabajo de `orm.QB.ReadOne`, no de este repo.

## 3. Diseño del paquete `db` (raíz)

`module github.com/tinywasm/db`, `go 1.25.2`. Deps: `github.com/tinywasm/model`,
`github.com/tinywasm/fmt`.

### 3.1 `executor.go`

```go
package db

// Executor represents the database connection abstraction. It must remain compatible with
// sql.DB, sql.Tx, mocks, and WASM drivers.
type Executor interface {
	Exec(query string, args ...any) error
	QueryRow(query string, args ...any) Scanner
	Query(query string, args ...any) (Rows, error)
	Close() error
}

// Scanner represents a single row scanner.
type Scanner interface {
	Scan(dest ...any) error
}

// Rows represents an iterator over query results.
type Rows interface {
	Next() bool
	Scan(dest ...any) error
	Columns() ([]string, error)
	Close() error
	Err() error
}
```

### 3.2 `compiler.go`

```go
package db

import "github.com/tinywasm/model"

// Compiler converts agnostic Query values into engine-specific Plans. Each backend dialect
// (postgres, sqlite) implements this to render its own SQL.
type Compiler interface {
	Compile(q Query, m model.Model) (Plan, error)
}
```

### 3.3 `conn.go` — **nuevo, no existía en `orm`**

```go
package db

// Conn is what a storage backend implements: the union of Executor and Compiler. Every real
// backend (postgres, sqlt, indexdb) is a single concrete type satisfying both halves — Conn
// names that pairing so it travels as one value instead of two arguments that could be
// mismatched (an Executor from one backend paired with a Compiler from another is an illegal
// state that used to be representable as two constructor args; Conn makes it impossible).
type Conn interface {
	Executor
	Compiler
}
```

> Por qué importa: `mem.New()` y cualquier `Factory.New` de `db/conformance` devuelven `db.Conn`, no
> dos valores. Los backends futuros (`sqlt.Open`, `postgres.Open`) también devolverán `db.Conn`.

### 3.4 `tx.go`

```go
package db

// TxBoundExecutor represents an executor bound to a transaction.
type TxBoundExecutor interface {
	Executor
	Commit() error
	Rollback() error
}

// TxExecutor represents an executor that supports transactions. A Conn optionally implements
// this (type-assert it) to signal transaction support.
type TxExecutor interface {
	Executor
	BeginTx() (TxBoundExecutor, error)
}
```

> La orquestación (`func (db *DB) Tx(fn func(tx *DB) error) error`, con rollback automático en error)
> es ergonomía de `orm`, no un contrato — no la repliques aquí. Este archivo solo nombra la
> **capacidad** que un `Conn` transaccional expone.

### 3.5 `query.go`

```go
package db

// Action represents the type of database DML operation. Purely DML — no DDL here (see §2).
type Action int

const (
	ActionCreate Action = iota
	ActionReadOne
	ActionUpdate
	ActionDelete
	ActionReadAll
)

// Order represents a sort order for a query. Sealed value type — construct with Asc/Desc.
// (Public constructors are new here: in orm, Order only had private fields set by the query
// builder inside the same package. Now that Order crosses the orm↔db package boundary, both
// orm's builder AND db/conformance's raw-contract tests need a way to build one from outside.)
type Order struct {
	column string
	dir    string
}

func (o Order) Column() string { return o.column }
func (o Order) Dir() string    { return o.dir }

// Asc creates an ascending sort order for column.
func Asc(column string) Order { return Order{column: column, dir: "ASC"} }

// Desc creates a descending sort order for column.
func Desc(column string) Order { return Order{column: column, dir: "DESC"} }

// Query represents a database DML query to be compiled and run by a Conn. Compilers read
// these fields to build a Plan.
type Query struct {
	Action     Action
	Table      string
	Columns    []string
	Values     []any
	Conditions []Condition
	OrderBy    []Order
	GroupBy    []string
	Limit      int
	Offset     int
}
```

### 3.6 `conditions.go`

```go
package db

// Condition represents a filter for a query. Sealed value type constructed via helper functions.
type Condition struct {
	field    string
	operator string
	value    any
	logic    string
}

func (c Condition) Field() string    { return c.field }
func (c Condition) Operator() string { return c.operator }
func (c Condition) Value() any       { return c.value }
func (c Condition) Logic() string    { return c.logic }

// Eq creates a condition for checking equality.
func Eq(field string, value any) Condition {
	return Condition{field: field, operator: "=", value: value, logic: "AND"}
}

// Neq creates a condition for checking inequality.
func Neq(field string, value any) Condition {
	return Condition{field: field, operator: "!=", value: value, logic: "AND"}
}

// Gt creates a condition for checking if a value is greater than another.
func Gt(field string, value any) Condition {
	return Condition{field: field, operator: ">", value: value, logic: "AND"}
}

// Gte creates a condition for checking if a value is greater than or equal to another.
func Gte(field string, value any) Condition {
	return Condition{field: field, operator: ">=", value: value, logic: "AND"}
}

// Lt creates a condition for checking if a value is less than another.
func Lt(field string, value any) Condition {
	return Condition{field: field, operator: "<", value: value, logic: "AND"}
}

// Lte creates a condition for checking if a value is less than or equal to another.
func Lte(field string, value any) Condition {
	return Condition{field: field, operator: "<=", value: value, logic: "AND"}
}

// Like creates a condition for checking if a value matches a pattern.
func Like(field string, value any) Condition {
	return Condition{field: field, operator: "LIKE", value: value, logic: "AND"}
}

// In creates a condition for checking if a value is in a list of values.
func In(field string, value any) Condition {
	return Condition{field: field, operator: "IN", value: value, logic: "AND"}
}

// Or creates a condition with OR logic (wraps an existing condition).
func Or(c Condition) Condition {
	c.logic = "OR"
	return c
}

// IsNotNull creates a condition for checking if a value is not null.
func IsNotNull(field string) Condition {
	return Condition{field: field, operator: "IS NOT NULL", logic: "AND"}
}
```

### 3.7 `execution_plan.go`

```go
package db

// Plan describes how a Conn should run the operation: the compiled query and its arguments.
type Plan struct {
	Mode  Action
	Query string
	Args  []any
}
```

### 3.8 `errors.go`

```go
package db

import "github.com/tinywasm/fmt"

// ErrNoRows is the agnostic sentinel for "query returned no rows". Conn implementations
// (postgres, sqlt) must map their driver-specific no-rows error to this value so callers can
// detect it without importing database/sql. This is the raw contract sentinel; orm.QB.ReadOne
// translates it to the ergonomic orm.ErrNotFound — db itself never does that translation.
var ErrNoRows = fmt.Err("no", "rows")
```

### 3.9 `scan.go`

```go
package db

import . "github.com/tinywasm/fmt"

// ScanAny maps a JSON-decoded Go value (any) into a typed pointer. Used by host-side adapters
// (REST, SQLite driver) and by db/mem, where values come from json.Unmarshal-shaped data
// rather than from js.Value.
func ScanAny(v any, dest any) error {
	switch p := dest.(type) {
	case *string:
		if s, ok := v.(string); ok {
			*p = s
		}
	case *int:
		switch n := v.(type) {
		case float64:
			*p = int(n)
		case int64:
			*p = int(n)
		}
	case *int64:
		switch n := v.(type) {
		case float64:
			*p = int64(n)
		case int64:
			*p = n
		}
	case *float64:
		if n, ok := v.(float64); ok {
			*p = n
		}
	case *bool:
		if b, ok := v.(bool); ok {
			*p = b
		}
	case *[]byte:
		switch b := v.(type) {
		case []byte:
			*p = b
		case string:
			*p = []byte(b)
		}
	case *any:
		*p = v
	default:
		return Errf("db: unsupported scan type: %T", dest)
	}
	return nil
}
```

## 4. `db/conformance` — el contrato ejecutable, sobre la `Query` cruda

`package conformance`, importa `testing` + `db` + `model`. Mismo patrón que
`tinywasm/router/conformance`: un paquete no-`_test` que expone `Run(t, Factory)`.

**Decisión de diseño (la más importante de este plan, justifica bien si algo no cuadra):** este
conformance prueba el contrato **crudo** — construye valores `db.Query{...}` directamente y llama
`Compile`+`Exec`/`Query`+`Scan`, **sin** pasar por ningún query builder (porque el query builder no
existe en este repo — vive en `orm`, es una capa opcional, ver §2). Esto prueba exactamente lo que un
backend debe cumplir: compilar una `Query` y ejecutar un `Plan` correctamente. No dupliques lógica de
builder aquí; si una cláusula necesita "AND de dos condiciones", arma el slice `[]db.Condition`
directamente.

### 4.1 Modelo canónico (idéntico al `Widget` que ya existía en `orm/conformance` — no lo cambies)

```go
package conformance

import "github.com/tinywasm/model"

// Widget is the canonical record every backend is driven with. Its schema carries real DB
// metadata (types + PK) so SQL backends can CREATE TABLE it; mem ignores the metadata and
// stores by column name. Hand-written (conformance depends only on model, not ormc).
var WidgetModel = model.Definition{
	Name: "conformance_widget",
	Fields: model.Fields{
		{Name: "id", Type: model.Text(), DB: &model.FieldDB{PK: true}},
		{Name: "name", Type: model.Text(), NotNull: true},
		{Name: "qty", Type: model.Int(), NotNull: true},
		{Name: "active", Type: model.Bool(), NotNull: true},
	},
}

type Widget struct {
	Id     string
	Name   string
	Qty    int64
	Active bool
}

func (w *Widget) ModelName() string     { return WidgetModel.Name }
func (w *Widget) Schema() []model.Field { return WidgetModel.Fields }
func (w *Widget) Pointers() []any       { return []any{&w.Id, &w.Name, &w.Qty, &w.Active} }
func (w *Widget) IsNil() bool           { return w == nil }
func (w *Widget) EncodeFields(wr model.FieldWriter) {
	wr.String("id", w.Id)
	wr.String("name", w.Name)
	wr.Int("qty", w.Qty)
	wr.Bool("active", w.Active)
}
func (w *Widget) DecodeFields(r model.FieldReader) {
	if v, ok := r.String("id"); ok {
		w.Id = v
	}
	if v, ok := r.String("name"); ok {
		w.Name = v
	}
	if v, ok := r.Int("qty"); ok {
		w.Qty = v
	}
	if v, ok := r.Bool("active"); ok {
		w.Active = v
	}
}

var _ model.Model = (*Widget)(nil)
```

> Verifica las firmas exactas de `model.Text()/Int()/Bool()` y `FieldWriter`/`FieldReader` con
> `go doc github.com/tinywasm/model` antes de compilar — usa las reales si difieren de arriba.

### 4.2 Factory + Run

```go
package conformance

import "github.com/tinywasm/model"
import "github.com/tinywasm/db"
import "testing"

// Factory builds, for ONE clause, a fresh db.Conn whose Widget table already exists and is
// EMPTY. Schema setup is the backend's job, done OUTSIDE this DML contract (this suite never
// builds a CreateTable Query):
//   - mem:              auto-creates the table on first Create — New just returns mem.New().
//   - sqlite/postgres:  New runs ddlc.ExportDDL(models) (or ddl.CreateTable) before returning.
//   - indexdb:          New declares `models` as IndexedDB object stores up front.
// models are the record types the suite will exercise. Called once per clause → no cross-clause
// bleed.
type Factory struct {
	Name string
	New  func(t *testing.T, models ...model.Model) db.Conn
}

func Run(t *testing.T, f Factory) {
	if f.New == nil {
		t.Fatal("conformance: Factory.New is required")
	}
	t.Run("create_then_read_one_by_pk", func(t *testing.T) { createThenReadOneByPK(t, f) })
	t.Run("read_one_no_match_is_not_found", func(t *testing.T) { readOneNoMatchIsNotFound(t, f) })
	t.Run("read_all_returns_every_row", func(t *testing.T) { readAllReturnsEveryRow(t, f) })
	t.Run("read_all_filters_by_eq", func(t *testing.T) { readAllFiltersByEq(t, f) })
	t.Run("read_all_ands_two_conditions", func(t *testing.T) { readAllAndsTwoConditions(t, f) })
	t.Run("read_all_ors_conditions", func(t *testing.T) { readAllOrsConditions(t, f) })
	t.Run("read_all_orders_asc_and_desc", func(t *testing.T) { readAllOrdersAscDesc(t, f) })
	t.Run("read_all_applies_limit_and_offset", func(t *testing.T) { readAllLimitOffset(t, f) })
	t.Run("comparison_operators_filter", func(t *testing.T) { comparisonOperatorsFilter(t, f) })
	t.Run("in_operator_filters", func(t *testing.T) { inOperatorFilters(t, f) })
	t.Run("update_changes_matched_rows_only", func(t *testing.T) { updateChangesMatchedOnly(t, f) })
	t.Run("delete_removes_matched_rows_only", func(t *testing.T) { deleteRemovesMatchedOnly(t, f) })
}
```

### 4.3 Helpers crudos (reemplazan lo que antes hacía el query builder de `orm`)

```go
func setup(t *testing.T, f Factory, seed ...*Widget) db.Conn {
	t.Helper()
	conn := f.New(t, &Widget{}) // table already exists & empty — backend set it up, not this suite
	for _, w := range seed {
		if err := create(conn, w); err != nil {
			t.Fatalf("seed create(%+v): %v", w, err)
		}
	}
	return conn
}

// create mirrors what orm.DB.Create builds, minus the autoincrement-PK-skip branch (Widget's PK
// is a plain Text, never AutoInc — that branch is orm's concern and is tested there, not here).
func create(conn db.Conn, w *Widget) error {
	schema := w.Schema()
	values := model.ReadValues(schema, w.Pointers())
	columns := make([]string, len(schema))
	for i, f := range schema {
		columns[i] = f.Name
	}
	q := db.Query{Action: db.ActionCreate, Table: w.ModelName(), Columns: columns, Values: values}
	plan, err := conn.Compile(q, w)
	if err != nil {
		return err
	}
	return conn.Exec(plan.Query, plan.Args...)
}

func readOne(conn db.Conn, w *Widget, conds ...db.Condition) error {
	q := db.Query{Action: db.ActionReadOne, Table: w.ModelName(), Conditions: conds, Limit: 1}
	plan, err := conn.Compile(q, w)
	if err != nil {
		return err
	}
	return conn.QueryRow(plan.Query, plan.Args...).Scan(w.Pointers()...)
}

func readAll(conn db.Conn, w *Widget, conds []db.Condition, order []db.Order, limit, offset int) ([]*Widget, error) {
	q := db.Query{
		Action: db.ActionReadAll, Table: w.ModelName(),
		Conditions: conds, OrderBy: order, Limit: limit, Offset: offset,
	}
	plan, err := conn.Compile(q, w)
	if err != nil {
		return nil, err
	}
	rows, err := conn.Query(plan.Query, plan.Args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Widget
	for rows.Next() {
		var got Widget
		if err := rows.Scan(got.Pointers()...); err != nil {
			return nil, err
		}
		out = append(out, &got)
	}
	return out, rows.Err()
}

func update(conn db.Conn, w *Widget, conds ...db.Condition) error {
	schema := w.Schema()
	columns := make([]string, len(schema))
	for i, f := range schema {
		columns[i] = f.Name
	}
	q := db.Query{
		Action: db.ActionUpdate, Table: w.ModelName(),
		Columns: columns, Values: model.ReadValues(schema, w.Pointers()), Conditions: conds,
	}
	plan, err := conn.Compile(q, w)
	if err != nil {
		return err
	}
	return conn.Exec(plan.Query, plan.Args...)
}

func deleteRow(conn db.Conn, w *Widget, conds ...db.Condition) error {
	q := db.Query{Action: db.ActionDelete, Table: w.ModelName(), Conditions: conds}
	plan, err := conn.Compile(q, w)
	if err != nil {
		return err
	}
	return conn.Exec(plan.Query, plan.Args...)
}
```

### 4.4 Las 12 cláusulas (código completo — pégalo tal cual, es la traducción exacta de las
    cláusulas que ya corrían en `orm/conformance`, ahora sobre el contrato crudo)

```go
import "errors"

func createThenReadOneByPK(t *testing.T, f Factory) {
	conn := setup(t, f, &Widget{Id: "w1", Name: "alpha", Qty: 3, Active: true})
	var got Widget
	if err := readOne(conn, &got, db.Eq("id", "w1")); err != nil {
		t.Fatalf("readOne: %v", err)
	}
	if got.Name != "alpha" || got.Qty != 3 || got.Active != true {
		t.Errorf("round-trip mismatch: got %+v", got)
	}
}

func readOneNoMatchIsNotFound(t *testing.T, f Factory) {
	conn := setup(t, f)
	var got Widget
	err := readOne(conn, &got, db.Eq("id", "nonexistent"))
	if !errors.Is(err, db.ErrNoRows) {
		t.Errorf("expected db.ErrNoRows, got %v", err)
	}
}

func readAllReturnsEveryRow(t *testing.T, f Factory) {
	conn := setup(t, f,
		&Widget{Id: "w1", Name: "alpha", Qty: 3, Active: true},
		&Widget{Id: "w2", Name: "beta", Qty: 4, Active: false},
	)
	got, err := readAll(conn, &Widget{}, nil, nil, 0, 0)
	if err != nil {
		t.Fatalf("readAll: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 widgets, got %d", len(got))
	}
}

func readAllFiltersByEq(t *testing.T, f Factory) {
	conn := setup(t, f,
		&Widget{Id: "w1", Name: "alpha", Qty: 3, Active: true},
		&Widget{Id: "w2", Name: "beta", Qty: 4, Active: false},
	)
	got, err := readAll(conn, &Widget{}, []db.Condition{db.Eq("name", "alpha")}, nil, 0, 0)
	if err != nil {
		t.Fatalf("readAll: %v", err)
	}
	if len(got) != 1 || got[0].Id != "w1" {
		t.Errorf("expected only w1, got %+v", got)
	}
}

func readAllAndsTwoConditions(t *testing.T, f Factory) {
	conn := setup(t, f,
		&Widget{Id: "a", Name: "x", Qty: 1, Active: true},
		&Widget{Id: "b", Name: "x", Qty: 1, Active: false},
		&Widget{Id: "c", Name: "y", Qty: 1, Active: true},
	)
	conds := []db.Condition{db.Eq("name", "x"), db.Eq("active", true)}
	got, err := readAll(conn, &Widget{}, conds, nil, 0, 0)
	if err != nil {
		t.Fatalf("readAll: %v", err)
	}
	if len(got) != 1 || got[0].Id != "a" {
		t.Errorf("AND of two conditions must return only {a}; got %+v", got)
	}
}

func readAllOrsConditions(t *testing.T, f Factory) {
	conn := setup(t, f,
		&Widget{Id: "a", Name: "x", Qty: 1, Active: true},
		&Widget{Id: "b", Name: "y", Qty: 2, Active: false},
		&Widget{Id: "c", Name: "z", Qty: 3, Active: true},
	)
	conds := []db.Condition{db.Eq("name", "x"), db.Or(db.Eq("name", "y"))}
	got, err := readAll(conn, &Widget{}, conds, nil, 0, 0)
	if err != nil {
		t.Fatalf("readAll: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2, got %d", len(got))
	}
	if got[0].Id != "a" || got[1].Id != "b" {
		t.Errorf("expected a and b, got %+v", got)
	}
}

func readAllOrdersAscDesc(t *testing.T, f Factory) {
	conn := setup(t, f,
		&Widget{Id: "w1", Name: "alpha", Qty: 5, Active: true},
		&Widget{Id: "w2", Name: "beta", Qty: 2, Active: false},
		&Widget{Id: "w3", Name: "gamma", Qty: 8, Active: true},
	)
	gotAsc, err := readAll(conn, &Widget{}, nil, []db.Order{db.Asc("qty")}, 0, 0)
	if err != nil {
		t.Fatalf("readAll asc: %v", err)
	}
	if len(gotAsc) != 3 || gotAsc[0].Id != "w2" || gotAsc[1].Id != "w1" || gotAsc[2].Id != "w3" {
		t.Errorf("expected w2, w1, w3; got %+v", gotAsc)
	}
	gotDesc, err := readAll(conn, &Widget{}, nil, []db.Order{db.Desc("qty")}, 0, 0)
	if err != nil {
		t.Fatalf("readAll desc: %v", err)
	}
	if len(gotDesc) != 3 || gotDesc[0].Id != "w3" || gotDesc[1].Id != "w1" || gotDesc[2].Id != "w2" {
		t.Errorf("expected w3, w1, w2; got %+v", gotDesc)
	}
}

func readAllLimitOffset(t *testing.T, f Factory) {
	conn := setup(t, f,
		&Widget{Id: "w1", Name: "alpha", Qty: 1, Active: true},
		&Widget{Id: "w2", Name: "beta", Qty: 2, Active: false},
		&Widget{Id: "w3", Name: "gamma", Qty: 3, Active: true},
		&Widget{Id: "w4", Name: "delta", Qty: 4, Active: false},
	)
	got, err := readAll(conn, &Widget{}, nil, []db.Order{db.Asc("qty")}, 2, 1)
	if err != nil {
		t.Fatalf("readAll: %v", err)
	}
	if len(got) != 2 || got[0].Id != "w2" || got[1].Id != "w3" {
		t.Errorf("expected w2, w3; got %+v", got)
	}
}

func comparisonOperatorsFilter(t *testing.T, f Factory) {
	conn := setup(t, f,
		&Widget{Id: "w1", Name: "alpha", Qty: 1, Active: true},
		&Widget{Id: "w2", Name: "beta", Qty: 2, Active: false},
		&Widget{Id: "w3", Name: "gamma", Qty: 3, Active: true},
	)
	cases := []struct {
		name string
		cond db.Condition
		want []string
	}{
		{"Neq", db.Neq("qty", 2), []string{"w1", "w3"}},
		{"Gt", db.Gt("qty", 1), []string{"w2", "w3"}},
		{"Gte", db.Gte("qty", 2), []string{"w2", "w3"}},
		{"Lt", db.Lt("qty", 3), []string{"w1", "w2"}},
		{"Lte", db.Lte("qty", 2), []string{"w1", "w2"}},
	}
	for _, c := range cases {
		got, err := readAll(conn, &Widget{}, []db.Condition{c.cond}, nil, 0, 0)
		if err != nil {
			t.Fatalf("%s: readAll: %v", c.name, err)
		}
		if len(got) != len(c.want) {
			t.Errorf("%s: expected %d rows, got %d (%+v)", c.name, len(c.want), len(got), got)
			continue
		}
		for i, id := range c.want {
			if got[i].Id != id {
				t.Errorf("%s: expected %v, got %+v", c.name, c.want, got)
				break
			}
		}
	}
}

func inOperatorFilters(t *testing.T, f Factory) {
	conn := setup(t, f,
		&Widget{Id: "a", Name: "alpha", Qty: 1, Active: true},
		&Widget{Id: "b", Name: "beta", Qty: 2, Active: false},
		&Widget{Id: "c", Name: "gamma", Qty: 3, Active: true},
	)
	got, err := readAll(conn, &Widget{}, []db.Condition{db.In("id", []any{"a", "c"})}, nil, 0, 0)
	if err != nil {
		t.Fatalf("readAll: %v", err)
	}
	if len(got) != 2 || got[0].Id != "a" || got[1].Id != "c" {
		t.Errorf("In: expected a, c; got %+v", got)
	}
}

func updateChangesMatchedOnly(t *testing.T, f Factory) {
	conn := setup(t, f,
		&Widget{Id: "w1", Name: "alpha", Qty: 1, Active: true},
		&Widget{Id: "w2", Name: "beta", Qty: 2, Active: false},
	)
	m := &Widget{Name: "updated", Qty: 99, Active: true}
	if err := update(conn, m, db.Eq("id", "w1")); err != nil {
		t.Fatalf("update: %v", err)
	}
	var got1 Widget
	if err := readOne(conn, &got1, db.Eq("id", "w1")); err != nil {
		t.Fatalf("readOne w1: %v", err)
	}
	if got1.Name != "updated" || got1.Qty != 99 || got1.Active != true {
		t.Errorf("w1 was not correctly updated: %+v", got1)
	}
	var got2 Widget
	if err := readOne(conn, &got2, db.Eq("id", "w2")); err != nil {
		t.Fatalf("readOne w2: %v", err)
	}
	if got2.Name != "beta" || got2.Qty != 2 || got2.Active != false {
		t.Errorf("w2 was modified but shouldn't have been: %+v", got2)
	}
}

func deleteRemovesMatchedOnly(t *testing.T, f Factory) {
	conn := setup(t, f,
		&Widget{Id: "w1", Name: "alpha", Qty: 1, Active: true},
		&Widget{Id: "w2", Name: "beta", Qty: 2, Active: false},
	)
	if err := deleteRow(conn, &Widget{}, db.Eq("id", "w1")); err != nil {
		t.Fatalf("delete: %v", err)
	}
	var got1 Widget
	err := readOne(conn, &got1, db.Eq("id", "w1"))
	if !errors.Is(err, db.ErrNoRows) {
		t.Errorf("expected w1 to be deleted/not found, got err: %v", err)
	}
	var got2 Widget
	if err := readOne(conn, &got2, db.Eq("id", "w2")); err != nil {
		t.Errorf("expected w2 to still exist, got: %v", err)
	}
}
```

> Nota de organización: `import "tinywasm/fmt"` va arriba del archivo junto a `testing`/`model`/`db`, no
> repetido por función — lo separé aquí solo para mostrarte dónde entra en juego.

## 5. `db/mem` — backend en memoria de referencia

`package mem`, importa `db`, `model`, `github.com/tinywasm/fmt`. Un único tipo `engine` implementa a
la vez `db.Compiler` y `db.Executor` (+ `db.TxExecutor`/`db.TxBoundExecutor`, como no-ops: no hay
transacciones reales que deshacer en memoria, pero implementarlas permite que código que hace
`conn.(db.TxExecutor)` no falle). Funciona porque el consumidor siempre llama
`compiler.Compile(q, m)` **inmediatamente antes** de `exec.Exec/QueryRow/Query(...)` sobre el mismo
`db.Conn` en la misma goroutine: `Compile` guarda el `Query`+`model`; el `Exec/Query` lo consume. Cero
SQL, cero parsing.

**Es exactamente el motor que hoy vive en `orm/mock/memdb.go`** (ya reescrito sin `map`, ver
AGENTS.md), con dos cambios: el paquete pasa de `mock` a `mem`, y el constructor devuelve `db.Conn` en
vez de envolverlo en `*orm.DB` (el envoltorio ergonómico ya no es responsabilidad de este repo).

```go
package mem

import (
	"github.com/tinywasm/db"
	"github.com/tinywasm/fmt"
	"github.com/tinywasm/model"
)

// New returns a functional in-memory db.Conn. It interprets the structured db.Query
// (Create/ReadOne/ReadAll/Update/Delete + Conditions/OrderBy/Limit/Offset). It is THE double a
// leaf module uses to test round-trips without importing a real driver, and it proves
// db/conformance exactly like the real backends do.
func New() db.Conn {
	return &engine{}
}

// dbCell is one column/value pair. Rows and tables are plain slices scanned linearly — no Go
// map anywhere: TinyGo's map runtime is heavy and bloats the wasm binary, prohibited across
// tinywasm (see AGENTS.md). Table/row counts here are tiny (test fixtures), so a linear scan
// costs nothing in practice.
type dbCell struct {
	col string
	val any
}

// dbRow is an ordered set of cells. A dbRow value shares its backing array with whatever it
// was copied from, so rows handed back by match() alias the stored data: set() mutates them
// in place, giving Update/Delete the same "reference into storage" behavior a
// map[string]any used to give for free.
type dbRow []dbCell

func (r dbRow) get(col string) (any, bool) {
	for _, c := range r {
		if c.col == col {
			return c.val, true
		}
	}
	return nil, false
}

// set overwrites an existing column in place. Callers only ever set columns that Create
// already wrote, so every call takes the "found" branch — there is no append fallback.
func (r dbRow) set(col string, val any) {
	for i := range r {
		if r[i].col == col {
			r[i].val = val
			return
		}
	}
}

// dbTable is one named table's rows.
type dbTable struct {
	name string
	rows []dbRow
}

type engine struct {
	tables []dbTable
	lastQ  db.Query
	lastM  model.Model
}

func (e *engine) tableIndex(name string) int {
	for i := range e.tables {
		if e.tables[i].name == name {
			return i
		}
	}
	return -1
}

func (e *engine) Compile(q db.Query, m model.Model) (db.Plan, error) {
	e.lastQ, e.lastM = q, m
	return db.Plan{Mode: q.Action, Query: "mem", Args: q.Values}, nil
}

func (e *engine) Close() error { return nil }

func (e *engine) BeginTx() (db.TxBoundExecutor, error) {
	return e, nil
}

func (e *engine) Commit() error   { return nil }
func (e *engine) Rollback() error { return nil }

func (e *engine) Exec(query string, args ...any) error {
	q := e.lastQ
	switch q.Action {
	case db.ActionCreate:
		newRow := make(dbRow, 0, len(q.Columns))
		for i, col := range q.Columns {
			if i < len(q.Values) {
				newRow = append(newRow, dbCell{col: col, val: q.Values[i]})
			}
		}
		idx := e.tableIndex(q.Table)
		if idx == -1 {
			// Auto-vivifies the table on first insert. This is why the mem Factory in
			// db/conformance needs no DDL — it just returns mem.New().
			e.tables = append(e.tables, dbTable{name: q.Table})
			idx = len(e.tables) - 1
		}
		e.tables[idx].rows = append(e.tables[idx].rows, newRow)
	case db.ActionUpdate:
		// Consumers build q.Columns from m.Schema() in order, so q.Columns[i] and
		// schema[i] always name the same field — no lookup needed.
		schema := e.lastM.Schema()
		for _, row := range e.match(q.Table, q.Conditions) { // match returns rows aliasing storage
			for i, col := range q.Columns {
				if i < len(schema) && schema[i].IsPK() {
					continue // do not overwrite PK on update
				}
				if i < len(q.Values) {
					row.set(col, q.Values[i])
				}
			}
		}
	case db.ActionDelete:
		idx := e.tableIndex(q.Table)
		if idx == -1 {
			return nil
		}
		kept := e.tables[idx].rows[:0:0]
		for _, row := range e.tables[idx].rows {
			if !matchRow(row, q.Conditions) {
				kept = append(kept, row)
			}
		}
		e.tables[idx].rows = kept
	}
	return nil
}

func (e *engine) QueryRow(query string, args ...any) db.Scanner {
	q := e.lastQ
	rows := applyOffsetLimit(applyOrder(e.match(q.Table, q.Conditions), q.OrderBy), q.Offset, 1)
	if len(rows) == 0 {
		return &memScanner{err: db.ErrNoRows}
	}
	return &memScanner{row: rows[0], schema: e.lastM.Schema()}
}

func (e *engine) Query(query string, args ...any) (db.Rows, error) {
	q := e.lastQ
	rows := applyOffsetLimit(applyOrder(e.match(q.Table, q.Conditions), q.OrderBy), q.Offset, q.Limit)
	return &memRows{rows: rows, schema: e.lastM.Schema(), idx: -1}, nil
}

func (e *engine) match(table string, conds []db.Condition) []dbRow {
	idx := e.tableIndex(table)
	if idx == -1 {
		return nil
	}
	var out []dbRow
	for _, row := range e.tables[idx].rows {
		if matchRow(row, conds) {
			out = append(out, row)
		}
	}
	return out
}

// matchRow evaluates conds left-to-right; the first Logic() is ignored (mirrors real adapters).
func matchRow(row dbRow, conds []db.Condition) bool {
	if len(conds) == 0 {
		return true
	}
	res := evalCond(row, conds[0])
	for _, c := range conds[1:] {
		if c.Logic() == "OR" {
			res = res || evalCond(row, c)
		} else {
			res = res && evalCond(row, c)
		}
	}
	return res
}

func evalCond(row dbRow, c db.Condition) bool {
	v, ok := row.get(c.Field())
	switch c.Operator() {
	case "IS NOT NULL":
		return ok && v != nil
	case "IN":
		return inSlice(v, c.Value())
	case "LIKE":
		return likeMatch(toStr(v), toStr(c.Value()))
	case "=":
		return equalAny(v, c.Value())
	case "!=":
		return !equalAny(v, c.Value())
	case ">":
		return compareAny(v, c.Value()) > 0
	case ">=":
		return compareAny(v, c.Value()) >= 0
	case "<":
		return compareAny(v, c.Value()) < 0
	case "<=":
		return compareAny(v, c.Value()) <= 0
	}
	return false
}

func inSlice(v any, listVal any) bool {
	if listVal == nil {
		return false
	}
	switch l := listVal.(type) {
	case []any:
		for _, it := range l {
			if equalAny(v, it) {
				return true
			}
		}
	case []string:
		vs := toStr(v)
		for _, it := range l {
			if vs == it {
				return true
			}
		}
	case []int:
		vf, ok := toFloat(v)
		if !ok {
			return false
		}
		for _, it := range l {
			if vf == float64(it) {
				return true
			}
		}
	case []int64:
		vf, ok := toFloat(v)
		if !ok {
			return false
		}
		for _, it := range l {
			if vf == float64(it) {
				return true
			}
		}
	}
	return false
}

func applyOrder(rows []dbRow, orders []db.Order) []dbRow {
	for oi := len(orders) - 1; oi >= 0; oi-- { // stable, last key least significant
		col, desc := orders[oi].Column(), orders[oi].Dir() == "DESC"
		for i := 1; i < len(rows); i++ {
			for j := i; j > 0; j-- {
				lv, _ := rows[j-1].get(col)
				rv, _ := rows[j].get(col)
				cmp := compareAny(lv, rv)
				if desc {
					cmp = -cmp
				}
				if cmp <= 0 {
					break
				}
				rows[j-1], rows[j] = rows[j], rows[j-1]
			}
		}
	}
	return rows
}

func applyOffsetLimit(rows []dbRow, offset, limit int) []dbRow {
	if offset > 0 {
		if offset >= len(rows) {
			return nil
		}
		rows = rows[offset:]
	}
	if limit > 0 && limit < len(rows) {
		rows = rows[:limit]
	}
	return rows
}

type memScanner struct {
	row    dbRow
	schema []model.Field
	err    error
}

func (s *memScanner) Scan(dest ...any) error {
	if s.err != nil {
		return s.err
	}
	return scanInto(s.row, s.schema, dest)
}

type memRows struct {
	rows   []dbRow
	schema []model.Field
	idx    int
}

func (r *memRows) Next() bool             { r.idx++; return r.idx < len(r.rows) }
func (r *memRows) Scan(dest ...any) error { return scanInto(r.rows[r.idx], r.schema, dest) }
func (r *memRows) Columns() ([]string, error) {
	cols := make([]string, len(r.schema))
	for i, f := range r.schema {
		cols[i] = f.Name
	}
	return cols, nil
}
func (r *memRows) Close() error { return nil }
func (r *memRows) Err() error   { return nil }

func scanInto(row dbRow, schema []model.Field, dest []any) error {
	for i, f := range schema {
		if i >= len(dest) {
			break
		}
		if v, ok := row.get(f.Name); ok {
			if err := db.ScanAny(v, dest[i]); err != nil {
				return err
			}
		}
	}
	return nil
}

func toStr(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []byte:
		return string(x)
	default:
		return fmt.Convert(x).String()
	}
}

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case int:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	case float32:
		return float64(x), true
	case float64:
		return x, true
	}
	return 0, false
}

func equalAny(a, b any) bool {
	if as, ok := a.(string); ok {
		return as == toStr(b)
	}
	if ab, ok := a.(bool); ok {
		bb, _ := b.(bool)
		return ab == bb
	}
	if af, aok := toFloat(a); aok {
		if bf, bok := toFloat(b); bok {
			return af == bf
		}
	}
	return false
}

func compareAny(a, b any) int {
	if af, aok := toFloat(a); aok {
		if bf, bok := toFloat(b); bok {
			switch {
			case af < bf:
				return -1
			case af > bf:
				return 1
			default:
				return 0
			}
		}
	}
	sa, sb := toStr(a), toStr(b)
	switch {
	case sa < sb:
		return -1
	case sa > sb:
		return 1
	default:
		return 0
	}
}

// likeMatch supports SQL LIKE with '%' wildcards.
func likeMatch(s, pattern string) bool {
	if findHelper(pattern, "%") == -1 {
		return s == pattern
	}
	if pattern == "%" {
		return true
	}

	var segments []string
	var current []byte
	for i := 0; i < len(pattern); i++ {
		if pattern[i] == '%' {
			if len(current) > 0 {
				segments = append(segments, string(current))
				current = nil
			}
		} else {
			current = append(current, pattern[i])
		}
	}
	if len(current) > 0 {
		segments = append(segments, string(current))
	}

	hasPrefix := pattern[0] != '%'
	hasSuffix := pattern[len(pattern)-1] != '%'

	if len(segments) == 0 {
		return true
	}

	str := s
	for i, seg := range segments {
		if i == 0 && hasPrefix {
			if !hasPrefixHelper(str, seg) {
				return false
			}
			str = str[len(seg):]
			continue
		}
		if i == len(segments)-1 && hasSuffix {
			return hasSuffixHelper(str, seg)
		}
		idx := findHelper(str, seg)
		if idx == -1 {
			return false
		}
		str = str[idx+len(seg):]
	}
	return true
}

func hasPrefixHelper(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	return s[:len(prefix)] == prefix
}

func hasSuffixHelper(s, suffix string) bool {
	if len(s) < len(suffix) {
		return false
	}
	return s[len(s)-len(suffix):] == suffix
}

func findHelper(s, sub string) int {
	if len(sub) == 0 {
		return 0
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

var (
	_ db.Executor        = (*engine)(nil)
	_ db.Compiler        = (*engine)(nil)
	_ db.Conn            = (*engine)(nil)
	_ db.TxExecutor      = (*engine)(nil)
	_ db.TxBoundExecutor = (*engine)(nil)
	_ db.Scanner         = (*memScanner)(nil)
	_ db.Rows            = (*memRows)(nil)
)
```

## 6. `db/mock` — recorders (dobles que capturan la `Query`)

`package mock`, importa `db`, `model`. Estos son **dobles de test**, distintos de `db/mem`: `mem` es un
motor funcional (almacena filas de verdad); `mock` son grabadoras que capturan qué se les llamó, para
que un test pueda verificar "¿mi código construyó la `Query` correcta?" sin ejecutar nada de verdad.

Es exactamente lo que hoy vive en `orm/mock/recorders.go` — mismo código, `orm.` → `db.`:

```go
package mock

import (
	"github.com/tinywasm/db"
	"github.com/tinywasm/model"
)

// Executor captures execution calls.
type Executor struct {
	ExecutedQueries []string
	ExecutedArgs    [][]any
	ReturnExecErr   error
	ReturnQueryRow  db.Scanner
	ReturnQueryRows db.Rows
	ReturnQueryErr  error
	ReturnCloseErr  error
}

func (m *Executor) Exec(query string, args ...any) error {
	m.ExecutedQueries = append(m.ExecutedQueries, query)
	m.ExecutedArgs = append(m.ExecutedArgs, args)
	return m.ReturnExecErr
}

func (m *Executor) QueryRow(query string, args ...any) db.Scanner {
	m.ExecutedQueries = append(m.ExecutedQueries, query)
	m.ExecutedArgs = append(m.ExecutedArgs, args)
	if m.ReturnQueryRow == nil {
		return &Scanner{}
	}
	return m.ReturnQueryRow
}

func (m *Executor) Query(query string, args ...any) (db.Rows, error) {
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
	LastQuery  db.Query
	LastModel  model.Model
	ReturnPlan db.Plan
	ReturnErr  error
}

func (m *Compiler) Compile(q db.Query, model model.Model) (db.Plan, error) {
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

func (m *TxExecutor) BeginTx() (db.TxBoundExecutor, error) {
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
	_ db.Executor        = (*Executor)(nil)
	_ db.Compiler        = (*Compiler)(nil)
	_ db.Scanner         = (*Scanner)(nil)
	_ db.Rows            = (*Rows)(nil)
	_ model.Model        = (*Model)(nil)
	_ db.TxExecutor      = (*TxExecutor)(nil)
	_ db.TxBoundExecutor = (*TxBoundExecutor)(nil)
)
```

## 7. Tests del propio repo

### 7.1 Raíz (`db_test.go`, `package db`)

Cubre los constructores/getters de los tipos de valor — nadie más los prueba, son el contrato:

- `Eq/Neq/Gt/Gte/Lt/Lte/Like/In/IsNotNull`: `Operator()`/`Value()`/`Logic()` (default `"AND"`)
  correctos para cada uno.
- `Or(c)`: `Logic()` pasa a `"OR"`, el resto de campos no cambia.
- `Asc(col)`/`Desc(col)`: `Column()`/`Dir()` correctos (`"ASC"`/`"DESC"`).
- `ScanAny`: **las mismas cláusulas que hoy corren en `orm/tests/scan_test.go`** — `*string`, `*int`
  desde `float64`/`int64`, `*int64` desde ambos, `*float64`, `*bool`, `*[]byte` desde `[]byte`/
  `string`, `*any`, y el `default` (tipo no soportado) devuelve error.
- `Conn`: un assert de compilación (`var _ Conn = (*struct{ Executor; Compiler })(nil)` no compila
  directo por ser anónimo — más simple: verifica en un test que un tipo que implementa ambas
  interfaces satisface `Conn` instanciándolo contra `mem.New()` desde un test en `db_test.go` que
  importe `github.com/tinywasm/db/mem` — ojo, esto crea una dependencia de test raíz→submódulo, que es
  aceptable en Go (los tests no cuentan para el grafo de dependencia del paquete productivo) pero si
  prefieres evitarlo, basta con el `var _ db.Conn = (*engine)(nil)` que ya vive dentro de `mem`).

### 7.2 `db/conformance` corriendo contra `db/mem`

No se auto-prueba (necesita un `Factory`); pero **debes** añadir un `conformance_test.go` en
`db/mem` (`package mem`, importa `db/conformance`) que lo corra:

```go
package mem

import (
	"testing"

	"github.com/tinywasm/db"
	"github.com/tinywasm/db/conformance"
	"github.com/tinywasm/model"
)

func TestMemConformance(t *testing.T) {
	conformance.Run(t, conformance.Factory{
		Name: "mem",
		New: func(t *testing.T, models ...model.Model) db.Conn {
			return New()
		},
	})
}
```

Esto prueba dos cosas a la vez: que `db/conformance` en sí funciona (sin backend real que lo
verifique, el conformance nunca se ejecutaría), y que `db/mem` cumple el contrato que él mismo va a
servir de referencia para otros backends.

### 7.3 Cobertura 100% de `db/mem` y `db/mock`

`conformance.Run` cubre el motor (`engine`, `matchRow`, `evalCond`, `applyOrder`,
`applyOffsetLimit`, `scanInto`, helpers de comparación). **No** cubre los bordes ni los recorders.
Añade tests directos hasta 100%, mismo patrón que ya existe en el ecosistema
(`orm/mock/mock_test.go` — replícalo adaptado a `package mem` y `package mock`):

- **`db/mem`**: `LIKE` con `%` al inicio/medio/fin y literal exacto; `IS NOT NULL`;
  `compareAny`/`equalAny` con `bool` y `string`; `toFloat` con `int`/`int32`/`int64`/`float32`;
  `offset >= len(rows)` ⇒ `nil`; `Delete` sobre una tabla nunca creada ⇒ no-op (no panic — este caso
  existe porque `tableIndex` puede devolver `-1`); `Close`/`Commit`/`Rollback`/`BeginTx` en el motor;
  `Columns()` con schema real; `Scan` con `dest` más corto que el schema; `Scan` con tipo no soportado
  (error de `ScanAny` propagado); columna ausente en la fila (no error, se ignora).
- **`db/mock`**: `Executor.Exec/QueryRow/Query/Close` (ramas con y sin `Return*` inyectado);
  `Compiler.Compile` (rama `ReturnPlan.Query==""` y con plan puesto); `Scanner.Scan` (con/sin
  `ScanErr`); `Rows.Next/Scan/Columns/Close/Err`; `Model.*` (incl. `Validate` con/sin `ValidErr`);
  `TxExecutor.BeginTx` (ramas `BeginTxErr`, `Bound==nil`, `Bound` preexistente);
  `TxBoundExecutor.Commit/Rollback`.

> Comprueba con `gotest` + cobertura; añade casos hasta 100% de `db/mem` y `db/mock` por separado.

## 8. Criterios de aceptación

- `github.com/tinywasm/db` existe, `go 1.25.2`, deps **solo** `model`+`fmt`.
- Raíz: `Executor`/`Compiler`/`Conn`/`Scanner`/`Rows`/`TxExecutor`/`TxBoundExecutor`, `Query`/`Action`/
  `Order`(+`Asc`/`Desc`)/`Condition`(+`Eq/Neq/Gt/Gte/Lt/Lte/Like/In/Or/IsNotNull`)/`Plan`, `ErrNoRows`,
  `ScanAny`. **Cero DDL.** Compila bajo `//go:build wasm` y bajo TinyGo (`gotest -tinygo`).
  **Cero `map[K]V`** en todo el repo.
  **Cero query builder / `DB` ergonómico** — solo el contrato + tipos de valor.
- `github.com/tinywasm/db/conformance`: `Run(t, Factory)`, `Factory{Name, New}` (`New` devuelve
  `db.Conn`), modelo `Widget` exportado, 12 cláusulas DML construidas sobre `db.Query{}` crudo (sin
  builder). Importa solo `testing`+`db`+`model`.
- `github.com/tinywasm/db/mem`: `New() db.Conn` funcional, sin driver, sin `map`.
- `github.com/tinywasm/db/mock`: 7 recorders exportados sin *stutter* (`var _ db.X` asserts).
- `db/mem` corre `conformance.Run` verde y deja **100% de cobertura** en `db/mem` y en `db/mock` por
  separado.
- `db_test.go` (raíz) cubre `Condition`/`Order`/`ScanAny` al 100%.
- `gotest` verde en todo el módulo (incluye `gotest -tinygo`); `GOOS=js GOARCH=wasm go build ./...`
  limpio.
- Publicado con `gopush`. **No** se toca `orm`, `ddl`, ni ningún backend en este plan — eso lo
  despachan fases posteriores que consumirán `db@v0.0.1+`.

## 9. Etapas

| # | Etapa | Archivo(s) | Criterio |
|---|---|---|---|
| 1 | Contrato raíz | `executor.go`, `compiler.go`, `conn.go`, `tx.go` | interfaces §3.1–3.4 |
| 2 | Tipos de valor | `query.go`, `conditions.go`, `execution_plan.go` | `Query`/`Action`/`Order`+`Asc`/`Desc`/`Condition`+helpers/`Plan` |
| 3 | Sentinela + scan | `errors.go`, `scan.go` | `ErrNoRows`, `ScanAny` |
| 4 | Tests raíz | `db_test.go` | Condition/Order/ScanAny 100% |
| 5 | Modelo canónico | `conformance/model.go` | `Widget`+`WidgetModel`, `var _ model.Model` |
| 6 | Factory + Run + cláusulas | `conformance/conformance.go` | `Run`/`Factory`/helpers §4.3/12 `t.Run` §4.4 |
| 7 | Motor en memoria | `mem/mem.go` | `New() db.Conn` + engine + helpers §5 |
| 8 | Test mem + conformance | `mem/conformance_test.go`, `mem/*_test.go` | conformance verde, cobertura `mem` 100% |
| 9 | Recorders | `mock/recorders.go` | 7 tipos + asserts §6 |
| 10 | Test mock | `mock/mock_test.go` | cobertura `mock` 100% |
| 11 | Verificar + publicar | — | `gotest` + `gotest -tinygo` verdes; |

## 10. Cierre (ciclo de vida del plan)

La parte duradera del diseño (qué es `db`, por qué existe
separado de `orm`, cómo un backend futuro lo consume) se traslada como sección corta a
`README.md`/`docs/ARCHITECTURE.md` de este repo — mismo patrón que `orm`/`ddl`.
