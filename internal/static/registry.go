package static

import (
	. "github.com/lilybw/go-solid/shared/static"
)

type StaticRegistry struct {
	cfg StaticConfig
}

func NewStaticRegistry(cfg StaticConfig) (*StaticRegistry, error) {

	return &StaticRegistry{
		cfg: cfg,
	}, nil
}

// Traverse directory
