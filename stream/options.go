package stream

const defaultSubscriptionDepth = 32

// SubscribeOption configures a subscription.
type SubscribeOption func(*subscribeOptions)

type subscribeOptions struct {
	depth int
}

// WithDepth sets the buffer depth (number of chunks) for a subscription.
func WithDepth(n int) SubscribeOption {
	return func(o *subscribeOptions) {
		if n > 0 {
			o.depth = n
		}
	}
}
