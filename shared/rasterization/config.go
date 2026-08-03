package rasterization

import "github.com/lilybw/go-solid/internal/meta"

type RasterizationConfig struct {
	// Either where to place the rasterized components, or where to find them if they have already been rasterized.
	// Defaults to go_solid.Config#Workspace
	Location meta.AbsoluteDirectoryPath
	// If true, the rasterization process will be considered complete when all components have been rasterized and written to disk.
	// Will check and validate that at least one component has been rasterized at the expected location
	ExpectCompleted bool
}

var NIL_RASTERIZATION_CONFIG = &RasterizationConfig{
	Location:        "",
	ExpectCompleted: false,
}
