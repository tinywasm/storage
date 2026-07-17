package storage

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
