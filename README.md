# tinywasm/storage
<img src="docs/img/badges.svg">

Storage port for tinywasm: Executor/Compiler contract, DML value types, conformance and in-memory reference backend.

## Overview

`storage` defines the foundational storage port for the `tinywasm` ecosystem. It specifies standard database driver interfaces (`Executor` & `Compiler`, unified as `Conn`) and agnostic DML value structures (e.g., `Query`, `Condition`, `Order`, `Plan`) that cross the boundary between query builders and drivers. It is designed specifically to be fully isomorphic and compatible with standard Go and TinyGo (`GOOS=js GOARCH=wasm`).

## API Usage Documentation

This library provides raw DML constructs and interfaces without relying on a full ORM or heavy database drivers.

### 1. Connecting and Query Compilation

Use `storage.Conn` to execute and compile actions. Every backend driver (like `sqlt` or `storage/mem`) implements `storage.Conn`:

```go
import (
	"github.com/tinywasm/storage"
	"github.com/tinywasm/storage/mem"
)

func main() {
	// Initialize the reference in-memory storage connection
	var conn storage.Conn = mem.New()
	defer conn.Close()
}
```

### 2. Formulating Queries and Conditions

Queries are represented as agnostic structured values using `storage.Query`:

```go
// Define a query to read widgets where qty is greater than 10 and sorting by name descending
q := storage.Query{
	Action: storage.ActionReadAll,
	Table:  "widgets",
	Conditions: []storage.Condition{
		storage.Gt("qty", 10),
		storage.Eq("active", true),
	},
	OrderBy: []storage.Order{
		storage.Desc("name"),
	},
	Limit: 5,
}
```

Available conditions constructors:
- `storage.Eq(field, value)`
- `storage.Neq(field, value)`
- `storage.Gt(field, value)`
- `storage.Gte(field, value)`
- `storage.Lt(field, value)`
- `storage.Lte(field, value)`
- `storage.Like(field, pattern)`
- `storage.In(field, slice)`
- `storage.Or(condition)` (wraps an existing condition with OR logic)
- `storage.IsNotNull(field)`

### 3. Compiling and Executing Plans

Pass a `storage.Query` and its `model.Model` target to a `Compiler` to compile it into an engine-specific `Plan`, and execute it with the `Executor`:

```go
// Compile the query against a model schema target
plan, err := conn.Compile(q, myModel)
if err != nil {
	log.Fatal(err)
}

// execute the query and retrieve rows iterator
rows, err := conn.Query(plan.Query, plan.Args...)
if err != nil {
	log.Fatal(err)
}
defer rows.Close()

for rows.Next() {
	var id string
	var name string
	var qty int64
	var active bool

	// scan values into destination pointers
	if err := rows.Scan(&id, &name, &qty, &active); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Row: %s %s %d %t\n", id, name, qty, active)
}
```

### 4. Direct Operations (`Exec` and `QueryRow`)

```go
// For Create/Update/Delete operations
plan, _ := conn.Compile(insertQuery, myModel)
err := conn.Exec(plan.Query, plan.Args...)

// For scanning a single row
plan, _ = conn.Compile(singleQuery, myModel)
var name string
err = conn.QueryRow(plan.Query, plan.Args...).Scan(&name)
if errors.Is(err, storage.ErrNoRows) {
	fmt.Println("No rows matched the criteria")
}
```

### 5. Transactions

Backends that support transactions optionally implement `storage.TxExecutor`. You can type-assert and use it as follows:

```go
if txConn, ok := conn.(storage.TxExecutor); ok {
	tx, err := txConn.BeginTx()
	if err != nil {
		log.Fatal(err)
	}
	defer tx.Rollback()

	// Execute on transaction
	tx.Exec("INSERT INTO widgets ...", ...)

	tx.Commit()
}
```

### 6. Mapping Scanned Values via `ScanAny`

The package includes a robust zero-reflection conversion function `ScanAny(v any, dest any) error` which converts JSON-decoded types (e.g. string, float64, boolean) safely into typed destination pointers. It is automatically utilized by `storage/mem` and other compliant drivers.

```go
var dest int64
// converts raw float64 value from json decode safely into int64 pointer
err := storage.ScanAny(float64(42), &dest)
```

## Running Tests

Tests are centralized in the `tests/` directory and can be executed via the `gotest` command:

```bash
gotest
```

WASM compilation compatibility is checked via:

```bash
GOOS=js GOARCH=wasm go build ./...
```
