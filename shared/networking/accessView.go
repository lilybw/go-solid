package networking

import (
	"github.com/lilybw/go-solid/shared/meta"
)

type LimitedAccessView interface {
	PutDataIsland(key meta.HTMLElementID, data meta.JSONString) LimitedAccessView
}
