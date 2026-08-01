package networking

import (
	"net/http"

	caching "github.com/lilybw/go_solid/internal/caching"
	"github.com/lilybw/go_solid/internal/meta"
)

// What to do when a failure occurs during the render call. Never expects an error to be returned, but if one is, it may panic.
type FailureCaseHandler func(w http.ResponseWriter, r *http.Request, err error) error

type RequestBehaviourBuilder interface {
	SetWriter(w http.ResponseWriter) RequestBehaviourBuilder
	SetRequest(r *http.Request) RequestBehaviourBuilder
	UponPropsMarshalingError(fn FailureCaseHandler) RequestBehaviourBuilder
	UponRegistryReloadError(fn FailureCaseHandler) RequestBehaviourBuilder
	UponRegistryLookupFailure(fn FailureCaseHandler) RequestBehaviourBuilder
	UponEntryGenerationError(fn FailureCaseHandler) RequestBehaviourBuilder
	UponTempEntryWriteError(fn FailureCaseHandler) RequestBehaviourBuilder
	UponCompBundlingError(fn FailureCaseHandler) RequestBehaviourBuilder
	SetSuccessCode(code int) RequestBehaviourBuilder
	TransmitRenderedTemplate(fn func(w http.ResponseWriter, r *http.Request, rendered *caching.Rendered) error) RequestBehaviourBuilder
}

type requestBehaviourBuilder struct {
	data *RequestBehaviour
}

func NewRequestBehaviourBuilder(data *RequestBehaviour) RequestBehaviourBuilder {
	return &requestBehaviourBuilder{
		data: data,
	}
}

func (this *requestBehaviourBuilder) SetWriter(w http.ResponseWriter) RequestBehaviourBuilder {
	meta.PanicIfTrue(w == nil, "SetWriter: writer cannot be nil")
	this.data.W = w
	return this
}

func (this *requestBehaviourBuilder) SetRequest(r *http.Request) RequestBehaviourBuilder {
	meta.PanicIfTrue(r == nil, "SetRequest: request cannot be nil")
	this.data.R = r
	return this
}

func (this *requestBehaviourBuilder) UponPropsMarshalingError(fn FailureCaseHandler) RequestBehaviourBuilder {
	meta.PanicIfTrue(fn == nil, "UponPropsMarshalingError handler cannot be nil")
	this.data.UponPropsMarshalingError = this.data.bind(fn)
	return this
}

func (this *requestBehaviourBuilder) UponRegistryReloadError(fn FailureCaseHandler) RequestBehaviourBuilder {
	meta.PanicIfTrue(fn == nil, "UponRegistryReloadError handler cannot be nil")
	this.data.UponRegistryReloadError = this.data.bind(fn)
	return this
}

func (this *requestBehaviourBuilder) UponRegistryLookupFailure(fn FailureCaseHandler) RequestBehaviourBuilder {
	meta.PanicIfTrue(fn == nil, "UponRegistryLookupFailure handler cannot be nil")
	this.data.UponRegistryLookupFailure = this.data.bind(fn)
	return this
}

func (this *requestBehaviourBuilder) UponEntryGenerationError(fn FailureCaseHandler) RequestBehaviourBuilder {
	meta.PanicIfTrue(fn == nil, "UponEntryGenerationError handler cannot be nil")
	this.data.UponEntryGenerationError = this.data.bind(fn)
	return this
}

func (this *requestBehaviourBuilder) UponTempEntryWriteError(fn FailureCaseHandler) RequestBehaviourBuilder {
	meta.PanicIfTrue(fn == nil, "UponTempEntryWriteError handler cannot be nil")
	this.data.UponTempEntryWriteError = this.data.bind(fn)
	return this
}

func (this *requestBehaviourBuilder) UponCompBundlingError(fn FailureCaseHandler) RequestBehaviourBuilder {
	meta.PanicIfTrue(fn == nil, "UponCompBundlingError handler cannot be nil")
	this.data.UponCompBundlingError = this.data.bind(fn)
	return this
}

func (this *requestBehaviourBuilder) TransmitRenderedTemplate(fn func(w http.ResponseWriter, r *http.Request, rendered *caching.Rendered) error) RequestBehaviourBuilder {
	meta.PanicIfTrue(fn == nil, "TransmitRenderedTemplate handler cannot be nil")
	this.data.TransmitRenderedTemplate = func(rendered *caching.Rendered) error {
		return fn(this.data.W, this.data.R, rendered)
	}
	return this
}

func (this *requestBehaviourBuilder) SetSuccessCode(code int) RequestBehaviourBuilder {
	this.data.SuccessCode = code
	return this
}

type RequestBehaviour struct {
	W                         http.ResponseWriter
	R                         *http.Request
	UponPropsMarshalingError  RequestBoundFailureCaseHandler
	UponRegistryReloadError   RequestBoundFailureCaseHandler
	UponRegistryLookupFailure RequestBoundFailureCaseHandler
	UponEntryGenerationError  RequestBoundFailureCaseHandler
	UponTempEntryWriteError   RequestBoundFailureCaseHandler
	UponCompBundlingError     RequestBoundFailureCaseHandler
	TransmitRenderedTemplate  func(rendered *caching.Rendered) error
	SuccessCode               int
}

// A failure case handler encapsulated with writer and request
type RequestBoundFailureCaseHandler func(err error) error

func (this *RequestBehaviour) bind(fn FailureCaseHandler) RequestBoundFailureCaseHandler {
	return func(err error) error {
		return fn(this.W, this.R, err)
	}
}

func NewRequestData(w http.ResponseWriter, r *http.Request) *RequestBehaviour {
	instance := &RequestBehaviour{
		W:                         w,
		R:                         r,
		SuccessCode:               200,
		UponPropsMarshalingError:  defaultHttpErrorHandler500(w, "Failed to marshal props"),
		UponRegistryReloadError:   defaultHttpErrorHandler500(w, "Failed to reload registry"),
		UponRegistryLookupFailure: defaultHttpErrorHandler500(w, "Component not found in registry"),
		UponEntryGenerationError:  defaultHttpErrorHandler500(w, "Failed to generate entry"),
		UponTempEntryWriteError:   defaultHttpErrorHandler500(w, "Failed to write temporary entry"),
		UponCompBundlingError:     defaultHttpErrorHandler500(w, "Failed to bundle component"),
	}
	instance.TransmitRenderedTemplate = func(rendered *caching.Rendered) error {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(instance.SuccessCode)
		_, err := w.Write([]byte(rendered.HTML))
		return err
	}
	return instance
}

// Prepend msg
func defaultHttpErrorHandler500(w http.ResponseWriter, msg string) RequestBoundFailureCaseHandler {
	return func(err error) error {
		http.Error(w, msg+": "+err.Error(), http.StatusInternalServerError)
		return nil
	}
}
