package storage

// Conn is what a storage backend implements: the union of Executor and Compiler. Every real
// backend (postgres, sqlt, indexdb) is a single concrete type satisfying both halves — Conn
// names that pairing so it travels as one value instead of two arguments that could be
// mismatched (an Executor from one backend paired with a Compiler from another is an illegal
// state that used to be representable as two constructor args; Conn makes it impossible).
type Conn interface {
	Executor
	Compiler
}
