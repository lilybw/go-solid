package networking

// SpecializedHandlerMode controls how Add inserts a handler.
type SpecializedHandlerMode int

const (
	HANDLER_MODE_INVALID SpecializedHandlerMode = iota
	HANDLER_MODE_REPLACE
	HANDLER_MODE_PREFIX
	HANDLER_MODE_POSTFIX
	HANDLER_MODE_PARALLEL
)
