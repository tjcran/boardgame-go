package redis

import (
	"context"
	"errors"

	"github.com/redis/go-redis/v9"

	"github.com/tjcran/boardgame-go/storage"
)

// DefaultInvalidationChannel is the Redis pub/sub channel used by
// Invalidator when InvalidatorOpts.Channel is empty.
const DefaultInvalidationChannel = "bgio:invalidate"

// InvalidatorOpts configures NewInvalidator.
type InvalidatorOpts struct {
	// Client is the go-redis client. Required.
	Client *redis.Client
	// Channel is the pub/sub channel name. Defaults to
	// DefaultInvalidationChannel.
	Channel string
}

// Invalidator implements storage.Invalidator over Redis PUB/SUB.
//
// Use one per server instance. Publish from the same node that wrote
// the underlying storage; Run on the same node subscribed to invalidate
// its local Cached.
type Invalidator struct {
	client  *redis.Client
	channel string
}

// NewInvalidator constructs the invalidator.
func NewInvalidator(opts InvalidatorOpts) (*Invalidator, error) {
	if opts.Client == nil {
		return nil, errors.New("redis invalidator: Options.Client is required")
	}
	channel := opts.Channel
	if channel == "" {
		channel = DefaultInvalidationChannel
	}
	return &Invalidator{client: opts.Client, channel: channel}, nil
}

// Publish broadcasts a cache-bust for matchID.
func (i *Invalidator) Publish(matchID string) error {
	return i.client.Publish(context.Background(), i.channel, matchID).Err()
}

// Run subscribes and invokes fn for each invalidation. Blocks until ctx
// is cancelled or the subscription errors.
func (i *Invalidator) Run(ctx context.Context, fn func(matchID string)) error {
	sub := i.client.Subscribe(ctx, i.channel)
	defer sub.Close()
	ch := sub.Channel()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok := <-ch:
			if !ok {
				return errors.New("redis invalidator: subscription channel closed")
			}
			fn(msg.Payload)
		}
	}
}

// Ensure interface satisfaction at compile time.
var _ storage.Invalidator = (*Invalidator)(nil)
