package fn

// Construct a function that takes in 2 parameters and returns the first
//
// k, v -> k
func First[T any, K any]() func(T, K) T {
	return func(t T, k K) T { return t }
}
