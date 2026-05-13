// Package redis is a Redis-backed storage adapter for boardgame-go.
// Useful when you need fast, shared state across many server instances
// — Redis is a natural fit for boardgame.io-style workloads (small,
// hot per-match state with high read/write churn).
//
// Key layout (all under a configurable prefix, default "bgio:"):
//
//	{prefix}match:{id}         → JSON-encoded Match
//	{prefix}matches:{game}     → SET of match IDs for List(game)
//	{prefix}matches:*all*      → SET of every match ID for List("")
//
// Wipe deletes the match key and removes the ID from both sets. The
// per-game set is the price of List support; if you don't List, the
// extra set ops are still O(1) per write.
package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/tjcran/boardgame-go/storage"
)

const (
	// DefaultPrefix is the key prefix used when Options.Prefix is empty.
	DefaultPrefix = "bgio:"
	// allMatchesKey is the per-game-agnostic set suffix.
	allMatchesKey = "matches:*all*"
)

// Options configures the Redis adapter.
type Options struct {
	// Client is the go-redis client. Required.
	Client *redis.Client
	// Prefix prepended to every key. Defaults to DefaultPrefix.
	Prefix string
	// OpTimeout caps each Redis call. Defaults to 5s.
	OpTimeout time.Duration
}

// Storage is the Redis implementation of storage.Storage.
type Storage struct {
	c       *redis.Client
	prefix  string
	timeout time.Duration
}

// New constructs a Redis-backed storage from a go-redis client.
// Typical usage:
//
//	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
//	store := redisstore.New(redisstore.Options{Client: rdb})
func New(opts Options) (*Storage, error) {
	if opts.Client == nil {
		return nil, errors.New("redis storage: Options.Client is required")
	}
	prefix := opts.Prefix
	if prefix == "" {
		prefix = DefaultPrefix
	}
	timeout := opts.OpTimeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	return &Storage{c: opts.Client, prefix: prefix, timeout: timeout}, nil
}

func (s *Storage) matchKey(id string) string         { return s.prefix + "match:" + id }
func (s *Storage) gameSetKey(name string) string     { return s.prefix + "matches:" + name }
func (s *Storage) allMatchesSetKey() string          { return s.prefix + allMatchesKey }

func (s *Storage) ctx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), s.timeout)
}

func (s *Storage) Create(m *storage.Match) error {
	payload, err := json.Marshal(m)
	if err != nil {
		return err
	}
	ctx, cancel := s.ctx()
	defer cancel()
	// NX guarantees we don't overwrite an existing match.
	ok, err := s.c.SetNX(ctx, s.matchKey(m.ID), payload, 0).Result()
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("redis storage: match %s already exists", m.ID)
	}
	// Index this ID under its game and the all-games set.
	if err := s.c.SAdd(ctx, s.gameSetKey(m.GameName), m.ID).Err(); err != nil {
		return err
	}
	return s.c.SAdd(ctx, s.allMatchesSetKey(), m.ID).Err()
}

func (s *Storage) Get(id string) (*storage.Match, error) {
	ctx, cancel := s.ctx()
	defer cancel()
	raw, err := s.c.Get(ctx, s.matchKey(id)).Bytes()
	if err == redis.Nil {
		return nil, storage.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var m storage.Match
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("redis decode %s: %w", id, err)
	}
	return &m, nil
}

func (s *Storage) Update(m *storage.Match) error {
	ctx, cancel := s.ctx()
	defer cancel()
	// Existence check; cheaper than always re-SADD-ing the indices.
	exists, err := s.c.Exists(ctx, s.matchKey(m.ID)).Result()
	if err != nil {
		return err
	}
	if exists == 0 {
		return storage.ErrNotFound
	}
	payload, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return s.c.Set(ctx, s.matchKey(m.ID), payload, 0).Err()
}

func (s *Storage) List(gameName string) ([]*storage.Match, error) {
	ctx, cancel := s.ctx()
	defer cancel()
	setKey := s.allMatchesSetKey()
	if gameName != "" {
		setKey = s.gameSetKey(gameName)
	}
	ids, err := s.c.SMembers(ctx, setKey).Result()
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = s.matchKey(id)
	}
	raws, err := s.c.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}
	out := make([]*storage.Match, 0, len(raws))
	for _, r := range raws {
		s2, ok := r.(string)
		if !ok || s2 == "" {
			continue
		}
		var m storage.Match
		if err := json.Unmarshal([]byte(s2), &m); err != nil {
			continue
		}
		out = append(out, &m)
	}
	return out, nil
}

func (s *Storage) Wipe(id string) error {
	ctx, cancel := s.ctx()
	defer cancel()
	// Fetch the game name first so we know which game-set to remove from.
	m, err := s.Get(id)
	if err != nil {
		return err
	}
	pipe := s.c.TxPipeline()
	pipe.Del(ctx, s.matchKey(id))
	pipe.SRem(ctx, s.gameSetKey(m.GameName), id)
	pipe.SRem(ctx, s.allMatchesSetKey(), id)
	_, err = pipe.Exec(ctx)
	return err
}
