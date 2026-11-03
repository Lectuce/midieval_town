package world

import "errors"

var ErrWorldRevisionConflict = errors.New(
	"world revision conflict",
)
