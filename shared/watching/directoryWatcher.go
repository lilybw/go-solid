package watching

import (
	"github.com/fsnotify/fsnotify"
	"github.com/lilybw/go-solid/internal/meta"
	"github.com/lilybw/go-solid/internal/noop"
)

type OnCreationCallback[T any] func(file meta.AbsoluteFilePath, derived T) error
type onDeletionCallback[T any] func(file meta.AbsoluteFilePath, derived T) error
type OnMutationCallback[T any] func(file meta.AbsoluteFilePath, derived T) error

type DWConfig[T any] struct {
	OnCreation       OnCreationCallback[T]
	OnDeletion       onDeletionCallback[T]
	OnMutation       OnMutationCallback[T]
	OnErr            func(error)
	DeriveAndInclude func(event fsnotify.Event) T
}

type DWVoidConfig = DWConfig[meta.Void]

var NIL_DW_CONFIG = &DWConfig[meta.Void]{
	OnCreation:       noop.TR_o_Err[string, meta.Void](),
	OnDeletion:       noop.TR_o_Err[string, meta.Void](),
	OnMutation:       noop.TR_o_Err[string, meta.Void](),
	OnErr:            noop.T_o_Void[error](),
	DeriveAndInclude: noop.T_o_R[fsnotify.Event](meta.VOID),
}
