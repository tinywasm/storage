# AGENTS.md — tinywasm/db

Working notes for AI agents operating in this library. For end-user docs see [README.md](README.md).
The implementation plan lives in [docs/PLAN.md](docs/PLAN.md) — self-contained, inlines the exact
code to write (this repo is a fresh `gonew`, there is nothing to reference by import yet).

## Mission of this package

`tinywasm/db` is the **storage port** of the tinywasm ecosystem: the contract a storage backend
(`postgres`, `sqlt`, `indexdb`) must implement (`Executor`+`Compiler`, unified as `Conn`), the DML
value types that cross that boundary (`Query`/`Condition`/`Order`/`Plan`), an executable conformance
suite (`db/conformance`), an in-memory reference backend (`db/mem`), and test recorders (`db/mock`).

It is the exact equivalent of `database/sql/driver` in the Go stdlib. `tinywasm/orm` (the query
builder, `Create`/`Update`/`Delete`/`Where`/`ReadAll`) is the equivalent of `database/sql` — an
**optional ergonomic layer** on top of this contract, never the other way around. See
[`app-releases/docs/DB_PORT_PROPOSAL.md`](https://github.com/tinywasm/app-releases/blob/main/docs/DB_PORT_PROPOSAL.md)
for the full architectural reasoning.

**This package must be usable standalone**, without ever importing `orm`. A backend author, or `ddl`,
or a leaf module writing infrastructure code, talks to `db` directly.

## Architectural rules (do not violate)

### This is THE foundational isomorphic library — the strictest rules in the ecosystem apply

`db` is imported by every storage backend, including WASM-only ones (`indexdb`). There is no
"backend-only, gate it behind `!wasm`" escape hatch for anything in this repo — every file here must
compile clean under `GOOS=js GOARCH=wasm` **and** under TinyGo (`gotest -tinygo`). Verify both before
considering any change done.

### No Go `map` anywhere — no exceptions, not even "just this once"

**Never use a built-in `map[K]V`, in any file.** TinyGo's map runtime is heavy and adds meaningful,
unavoidable size to every wasm binary that imports this code, directly or transitively (which is
every backend and every app in the ecosystem — this is the most-imported package after `model` and
`fmt`). A map introduced here is a tax paid by the entire ecosystem, forever.

- For a **string→string** pair, use `github.com/tinywasm/fmt.KeyValue{Key, Value string}`.
- For anything else (a table's rows, a row's columns, a lookup by name), use a small local
  slice-of-structs scanned linearly. See `db/mem`'s `dbCell`/`dbRow`/`dbTable` (docs/PLAN.md §5) —
  every collection in this repo is tiny (one table's columns, one row's cells), so a linear scan
  costs nothing measurable.
- If you're tempted to add a map "just for a lookup cache," don't. Reach for a linear scan first, and
  only reconsider with a profiler backing you up, never on a hunch.

### No `database/sql`, no `reflect`, no query builder

- **No `database/sql` import, anywhere in this repo.** `db` is the agnostic contract; leaking a
  driver-specific type here would defeat the entire point of the port. Adapters (in their own repos)
  are responsible for translating `database/sql` semantics (e.g. `sql.ErrNoRows`) into this package's
  sentinels (`db.ErrNoRows`) — that translation happens in the adapter, never here.
- **No `reflect`.** All types in this package are plain structs with exported getter methods
  (`Condition.Field()`, `Order.Column()`, …). Struct tags, if ever relevant, are a build-time (`ormc`)
  concern — not this package's.
- **No query builder, no `DB` type, no `Where`/`OrderBy` fluent API.** That is `orm`'s job — an
  optional layer built on top of this contract, deliberately kept out of it (see DB_PORT_PROPOSAL.md
  §6.3/§6.4/§6.8: the builder is invariant glue written once, not part of what varies per backend).
  Do not add ergonomic sugar here "to make `db` nicer to use directly" — if it doesn't vary by
  backend, it doesn't belong in the contract.
- **No DDL.** `CreateTable`/`Sync`/schema management lives in `tinywasm/ddl`, a separate repo that
  consumes `db.Conn` + `db.Compiler`. This package has zero opinions about schema.
- **No DSN registry (`Open`/`Register`).** A string-keyed lookup that fails at runtime ("unknown
  scheme") is exactly the kind of thing the construction harness forbids (fail at compile time, not
  runtime). Backends expose typed constructors (`sqlt.Open(dsn) (db.Conn, error)`); assembly with the
  ergonomic layer is the consumer's explicit `orm.New(conn)` call. Do not add a registry here.

### `Conn` is the seam — never split it back into two arguments

`Conn interface { Executor; Compiler }` exists so a backend is passed around as **one** value. Every
real backend implements both halves in the same concrete type anyway (there is no such thing as an
`Executor` from one backend paired with a `Compiler` from another) — `Conn` makes that pairing the
only representable state. If you're writing a constructor or a `Factory` field, it takes/returns
`db.Conn`, not `(Executor, Compiler)`.

## Code layout

| File / Dir | Role |
|------------|------|
| `executor.go` | `Executor`, `Scanner`, `Rows` — what a backend runs a compiled `Plan` through |
| `compiler.go` | `Compiler` — translates a `Query` into a `Plan` |
| `conn.go` | `Conn` = `Executor` + `Compiler`, the single value a backend hands back |
| `tx.go` | `TxExecutor`, `TxBoundExecutor` — optional transaction capability |
| `query.go` | `Action`, `Order` (+`Asc`/`Desc`), `Query` — the DML value types |
| `conditions.go` | `Condition` + constructors (`Eq`, `Gt`, `In`, `Or`, `IsNotNull`, …) |
| `execution_plan.go` | `Plan` — what `Compile` produces and `Exec`/`Query` consumes |
| `errors.go` | `ErrNoRows` — the sentinel every backend must map its driver's no-rows error to |
| `scan.go` | `ScanAny` — typed value → pointer, used by `db/mem` and host-side adapters |
| `conformance/` | Executable DML contract (`Run(t, Factory)`), built on raw `Query` values — no builder |
| `mem/` | `mem.New() db.Conn` — functional in-memory reference backend, no map, no driver |
| `mock/` | Recorders (`mock.Executor`, `mock.Compiler`, …) — capture calls, don't execute anything |
| `docs/` | `PLAN.md` (self-contained implementation plan, delete after `gopush`), architecture notes |

## Testing

Install once:

```bash
go install github.com/tinywasm/devflow/cmd/gotest@latest
```

Run:

```bash
gotest              # vet + race + cover + wasm + badges
gotest -tinygo       # also compiles against the TinyGo compiler — mandatory for this repo
gotest -no-cache    # force re-run
gotest -run TestX   # filter
```

Publish with `gopush 'message'` (tests + tag + push) — never `git commit`/`git push` directly.

## Common mistakes to avoid

- Reaching for `map[K]V` anywhere → use `fmt.KeyValue` or a small local slice-of-structs scanned
  linearly instead. No exceptions, not even in a test helper.
- Adding a `Where`/`OrderBy`-style method to `Query`, `Condition`, or anything in this package →
  that's `orm`'s job. If it makes `db` "nicer to use directly," it's ergonomic sugar and belongs one
  layer up.
- Splitting `Conn` back into two constructor/factory arguments (`exec Executor, compiler Compiler`) →
  defeats the reason `Conn` exists. Take/return one `db.Conn`.
- Forgetting `gotest -tinygo` — a change that only passes `go test`/`GOOS=js GOARCH=wasm go build` but
  not TinyGo is not done. TinyGo's stdlib subset and map runtime behavior differ from both.
- Importing `database/sql` or any concrete driver "just to check a type" → never, not even in a test.
