package logging

type LogLevel uint

const (
	// LEVEL_UNSET is the zero value: a config that never named a level.
	// Normalization resolves it to DEFAULT_LEVEL.
	LEVEL_UNSET LogLevel = iota
	LEVEL_TRACE
	LEVEL_DEBUG
	LEVEL_INFO
	LEVEL_ERROR
	LEVEL_FATAL
)

// DEFAULT_LEVEL is what LEVEL_UNSET resolves to. A library should be quiet
// unless asked: set LogLevel explicitly to opt into the config dump and the
// rest of the diagnostic output.
const DEFAULT_LEVEL = LEVEL_ERROR
