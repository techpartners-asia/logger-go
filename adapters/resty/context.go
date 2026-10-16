package restylog

import (
	"context"
	"time"

	"resty.dev/v3"
)

func contextWithStart(r *resty.Request) context.Context {
	return context.WithValue(r.Context(), startKey{}, time.Now())
}
