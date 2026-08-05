package logging

type LogLevel uint

const (
	LEVEL_TRACE LogLevel = iota + 1
	LEVEL_DEBUG
	LEVEL_INFO
	LEVEL_ERROR
	LEVEL_FATAL
)
