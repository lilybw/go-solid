package networking

import "strconv"

// HandlerMode controls where HandlerMap.Add / AddType inserts a handler.
//
// A slot holds Chains: a set of chains dispatched concurrently, each chain run
// in order and stopped at its first error. Only PARALLEL adds a chain; every
// other mode edits chain 0, the primary chain. Adding a handler in PREFIX,
// POSTFIX or REPLACE mode therefore never disturbs chains added in PARALLEL
// mode.
type HandlerMode int

const (
	// HANDLER_MODE_INVALID is the zero value and is never usable: an
	// unset mode is a bug at the call site, not a silent default.
	HANDLER_MODE_INVALID HandlerMode = iota

	// HANDLER_MODE_REPLACE discards the primary chain and starts a new one
	// with this handler. Parallel chains are left intact.
	HANDLER_MODE_REPLACE

	// HANDLER_MODE_PREFIX runs this handler before the rest of the primary
	// chain, so it can abort the chain by returning an error.
	HANDLER_MODE_PREFIX

	// HANDLER_MODE_POSTFIX appends to the primary chain. This is the default
	// for RequestBehaviourBuilder.Upon.
	HANDLER_MODE_POSTFIX

	// HANDLER_MODE_PARALLEL gives this handler a chain of its own, dispatched
	// concurrently with every other chain in the slot. Use it for observers
	// that must not be able to abort anyone else's chain.
	HANDLER_MODE_PARALLEL
)

func (m HandlerMode) String() string {
	switch m {
	case HANDLER_MODE_INVALID:
		return "HANDLER_MODE_INVALID"
	case HANDLER_MODE_REPLACE:
		return "HANDLER_MODE_REPLACE"
	case HANDLER_MODE_PREFIX:
		return "HANDLER_MODE_PREFIX"
	case HANDLER_MODE_POSTFIX:
		return "HANDLER_MODE_POSTFIX"
	case HANDLER_MODE_PARALLEL:
		return "HANDLER_MODE_PARALLEL"
	default:
		return "HandlerMode(" + strconv.Itoa(int(m)) + ")"
	}
}
