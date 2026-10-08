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

type callKeyKey struct{}

// WithCallKey records the per-invocation key of the tool call ctx runs
// (loop.ToolCall.Key). Unlike the model's call ID it never repeats, so a
// permission prompt can bind its ask to exactly this call.
func WithCallKey(ctx context.Context, key string) context.Context {
	return context.WithValue(ctx, callKeyKey{}, key)
}

// CallKeyFromContext returns the key WithCallKey recorded, or "".
func CallKeyFromContext(ctx context.Context) string {
	k, _ := ctx.Value(callKeyKey{}).(string)
	return k
}
