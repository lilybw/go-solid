package watching

import (
	"github.com/fsnotify/fsnotify"
	"github.com/lilybw/go-solid/internal/noop"
	"github.com/lilybw/go-solid/shared/meta"
)

type OnCreationCallback[T any] func(file meta.AbsoluteFilePath, derived T) error
type OnDeletionCallback[T any] func(file meta.AbsoluteFilePath, derived T) error
type OnMutationCallback[T any] func(file meta.AbsoluteFilePath, derived T) error

type DWConfig[T any] struct {
	OnCreation       OnCreationCallback[T]
	OnDeletion       OnDeletionCallback[T]
	OnMutation       OnMutationCallback[T]
	OnErr            func(error)
	DeriveAndInclude func(event fsnotify.Event) T
}

type DWVoidConfig = DWConfig[meta.Void]

var NIL_DW_CONFIG = &DWConfig[meta.Void]{
	OnCreation:       noop.TR_o_Err[meta.AbsoluteFilePath, meta.Void](),
	OnDeletion:       noop.TR_o_Err[meta.AbsoluteFilePath, meta.Void](),
	OnMutation:       noop.TR_o_Err[meta.AbsoluteFilePath, meta.Void](),
	OnErr:            noop.T_o_Void[error](),
	DeriveAndInclude: noop.T_o_R[fsnotify.Event](meta.VOID),
}
