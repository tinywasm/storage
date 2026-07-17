package storage

// Plan describes how a Conn should run the operation: the compiled query and its arguments.
type Plan struct {
	Mode  Action
	Query string
	Args  []any
}
