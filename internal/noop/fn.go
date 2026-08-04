package noop

// because not having lambda syntax and a very pedantic type system sometimes gets annoying

func T_o_Void[T any]() func(T) {
	return func(_ T) {}
}

func TR_o_Err[T any, R any]() func(T, R) error {
	return func(t T, r R) error { return nil }
}

func T_o_R[T any, R any](r R) func(T) R {
	return func(_ T) R { return r }
}

func T_o_T[T any]() func(T) T {
	return func(t T) T { return t }
}
