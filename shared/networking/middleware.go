package networking

type Middleware = func(LimitedAccessView, *RequestBehaviour) error
