package internal

// Error codes returned by the process entry point. They start at 1 because
// there is no shared common module to continue an existing sequence from.
const (
	ErrCreateLogFile = iota + 1
	ErrStartServer
	ErrConfig
	ErrDatabase
	ErrPidLock
	ErrInvalidAuthentication
	ErrLast
)
