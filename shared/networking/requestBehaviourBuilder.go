package networking

import (
	"net/http"

	"github.com/lilybw/go-solid/internal/caching"
)

// What to do when a failure occurs during the render call. Never expects an error to be returned, but if one is, it may panic.
type FailureCaseHandler func(w http.ResponseWriter, r *http.Request, err error) error

type RequestBehaviourBuilder interface {
	SetWriter(w http.ResponseWriter) RequestBehaviourBuilder
	SetRequest(r *http.Request) RequestBehaviourBuilder
	UponPropsMarshalingError(fn FailureCaseHandler) RequestBehaviourBuilder
	UponRegistryLookupFailure(fn FailureCaseHandler) RequestBehaviourBuilder
	UponEntryGenerationError(fn FailureCaseHandler) RequestBehaviourBuilder
	UponTempEntryWriteError(fn FailureCaseHandler) RequestBehaviourBuilder
	UponCompBundlingError(fn FailureCaseHandler) RequestBehaviourBuilder
	SetSuccessCode(code int) RequestBehaviourBuilder
	TransmitRenderedTemplate(fn func(w http.ResponseWriter, r *http.Request, rendered *caching.Rendered) error) RequestBehaviourBuilder
}

type RequestBehaviour struct {
	W                         http.ResponseWriter
	R                         *http.Request
	UponPropsMarshalingError  RequestBoundFailureCaseHandler
	UponRegistryLookupFailure RequestBoundFailureCaseHandler
	UponEntryGenerationError  RequestBoundFailureCaseHandler
	UponTempEntryWriteError   RequestBoundFailureCaseHandler
	UponCompBundlingError     RequestBoundFailureCaseHandler
	TransmitRenderedTemplate  func(rendered *caching.Rendered) error
	SuccessCode               int
}

func (this *RequestBehaviour) Bind(fn FailureCaseHandler) RequestBoundFailureCaseHandler {
	return func(err error) error {
		return fn(this.W, this.R, err)
	}
}

// A failure case handler encapsulated with writer and request
type RequestBoundFailureCaseHandler func(err error) error
