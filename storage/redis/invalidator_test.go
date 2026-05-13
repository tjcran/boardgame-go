package redis_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"

	redisstore "github.com/tjcran/boardgame-go/storage/redis"
)

func TestInvalidatorPublishReachesSubscriber(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	clientA := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	defer clientA.Close()
	clientB := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	defer clientB.Close()

	pub, err := redisstore.NewInvalidator(redisstore.InvalidatorOpts{Client: clientA})
	if err != nil {
		t.Fatalf("publisher: %v", err)
	}
	sub, err := redisstore.NewInvalidator(redisstore.InvalidatorOpts{Client: clientB})
	if err != nil {
		t.Fatalf("subscriber: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var (
		mu       sync.Mutex
		received []string
		ready    = make(chan struct{})
	)

	go func() {
		// miniredis takes a tick to register the subscription; signal
		// ready after the channel is created.
		close(ready)
		_ = sub.Run(ctx, func(matchID string) {
			mu.Lock()
			received = append(received, matchID)
			mu.Unlock()
		})
	}()

	<-ready
	// Brief settle for the subscription to wire through miniredis.
	time.Sleep(50 * time.Millisecond)

	for _, id := range []string{"a", "b", "c"} {
		if err := pub.Publish(id); err != nil {
			t.Fatalf("publish %s: %v", id, err)
		}
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		got := len(received)
		mu.Unlock()
		if got == 3 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	t.Fatalf("expected 3 invalidations, got %v", received)
	mu.Unlock()
}
