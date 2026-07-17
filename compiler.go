package storage

import "github.com/tinywasm/model"

// Compiler converts agnostic Query values into engine-specific Plans. Each backend dialect
// (postgres, sqlite) implements this to render its own SQL.
type Compiler interface {
	Compile(q Query, m model.Model) (Plan, error)
}
