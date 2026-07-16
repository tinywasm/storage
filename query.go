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
