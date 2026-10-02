package tools

import "context"

type callIDKey struct{}

// WithCallID records the ID of the tool call ctx runs. The write tools file
// their checkpoints under it (2.0 F4), so a rewind can map them to
// messages.
func WithCallID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, callIDKey{}, id)
}

// CallIDFromContext returns the ID WithCallID recorded, or "".
func CallIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(callIDKey{}).(string)
	return id
}
