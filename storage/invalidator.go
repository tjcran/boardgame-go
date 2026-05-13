package storage

import "context"

// Invalidator is the interface paired with Cached for cross-node cache
// busts. One node calls Publish(matchID) after a write; subscribed
// nodes receive the matchID and drop their cached copy.
//
// Used as:
//
//	cached := storage.NewCached(inner, storage.CacheOpts{...})
//	inv := redisstore.NewInvalidator(...)
//	go inv.Run(ctx, cached.Invalidate)
//	// every write on this node also publishes:
//	defer inv.Publish(matchID)
//
// The interface lives here (in package storage) so the cache and any
// invalidator backend can share a contract without an import cycle.
type Invalidator interface {
	// Publish notifies every subscriber that the given match has been
	// updated remotely; their cache should drop the entry.
	Publish(matchID string) error

	// Run subscribes and calls fn for each incoming invalidation until
	// ctx is cancelled. Returns ctx.Err() on shutdown.
	Run(ctx context.Context, fn func(matchID string)) error
}
