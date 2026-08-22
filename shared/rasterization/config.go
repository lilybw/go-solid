package rasterization

import "github.com/lilybw/go-solid/internal/meta"

type RasterizationConfig struct {
	// Either where to place the rasterized components, or where to find them if they have already been rasterized.
	// Defaults to go_solid.Config#Workspace
	Location meta.AbsoluteDirectoryPath
	// If true, the rasterization process will be considered complete when all components have been rasterized and written to disk.
	// Will check and validate that at least one component has been rasterized at the expected location
	ExpectCompleted bool
	// Disabled turns the pre-render pass off. Phrased negatively so the zero
	// value keeps rasterization on, which is the default.
	//
	// A rasterization that was never configured — Config#Rasterization left nil
	// — disables itself when it cannot run: DisableCaching leaves it nowhere to
	// write, and Generation.Disabled leaves it nothing to build. An explicitly
	// provided config is taken at its word instead.
	Disabled bool
}

// Active reports whether the pre-render pass should run. Safe on a nil config.
func (c *RasterizationConfig) Active() bool { return c != nil && !c.Disabled }

var NIL_RASTERIZATION_CONFIG = &RasterizationConfig{
	Location:        "",
	ExpectCompleted: false,
	Disabled:        false,
}
