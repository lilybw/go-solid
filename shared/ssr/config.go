package ssr

// SSRConfig turns server-side rendering on and decides how strict it is.
//
// go_solid renders the markup it can prove derivable from props and leaves
// the rest to the client. Nothing is executed on the server: the compiler
// describes a component's markup, and a component whose markup depends on
// signals or stores is simply not rendered here.
type SSRConfig struct {
	// Disabled turns the pass off. Phrased negatively so the zero value keeps
	// server rendering on.
	Disabled bool

	// Strict makes a component that cannot be fully server-rendered an error
	// at Prepare rather than a silent fall back to client rendering. Use it
	// to keep a page from regressing to a blank first paint unnoticed.
	Strict bool
}

// Active reports whether server rendering should run. Safe on a nil config.
func (c *SSRConfig) Active() bool { return c != nil && !c.Disabled }

// IsStrict reports whether an unrenderable component is an error. Safe on a
// nil config.
func (c *SSRConfig) IsStrict() bool { return c != nil && !c.Disabled && c.Strict }

var NIL_SSR_CONFIG = &SSRConfig{Disabled: false, Strict: false}
