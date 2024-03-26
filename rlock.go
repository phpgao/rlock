package rlock

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/errgroup"
)

const (
	defaultMaxLockTime = 500 * time.Millisecond
	defaultClockDrift  = 0.05
	defaultRetryTimes  = 3
	charset            = "abcdefghijklmnopqrstuvwxyz" +
		"ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	luaScript = `if redis.call("get", KEYS[1]) == ARGV[1] then
	return redis.call("del", KEYS[1])
	else
	return 0
	end
	`
)

var (
	seededRand = rand.New(rand.NewSource(time.Now().UnixNano()))
)

type RLock struct {
	// lockID is the unique identifier for the distributed lock.
	lockID string

	// clients is a slice of Redis client instances for interacting with Redis nodes.
	clients []*redis.Client

	// limit defines the maximum number of concurrent lock operations that can be executed.
	limit int

	// critical is the threshold for the minimum number of successful operations needed to consider the lock action successful.
	critical uint32

	// mu is a mutex to protect concurrent access to the internal state map m.
	mu sync.Mutex

	// m holds the state of which key is locked by which value (lock value).
	m map[string]string

	// maxLockTime is the maximum duration to wait for the lock to be acquired before timing out.
	maxLockTime time.Duration

	// drift represents the clock drift tolerance when calculating lock validity time.
	drift float64

	// retry specifies the number of times lock acquisition should be attempted before failing.
	retry int

	// randomFunc is a function that generates a random string of a given length, used for lock value generation.
	randomFunc func(int) string
}

// NewRLock creates a new distributed lock instance.
func NewRLock(clients []*redis.Client, opts ...Opt) (*RLock, error) {
	if len(clients)%2 == 0 {
		return nil, fmt.Errorf("rock: invalid number of redis clients: %d", len(clients))
	}
	r := &RLock{
		clients:     clients,
		m:           make(map[string]string),
		mu:          sync.Mutex{},
		maxLockTime: defaultMaxLockTime,
		drift:       defaultClockDrift,
		retry:       defaultRetryTimes,
		randomFunc:  randomString,
	}

	for _, opt := range opts {
		opt(r)
	}

	r.critical = uint32(len(r.clients)/2 + 1)

	if r.limit == 0 {
		r.limit = runtime.GOMAXPROCS(0)
	}

	if r.lockID == "" {
		r.lockID = r.randomFunc(5)
	}

	return r, nil
}

// randomString generate random string
func randomString(length int) string {
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[seededRand.Intn(len(charset))]
	}
	return string(b)
}

type Opt func(*RLock)

// WithMaxLockTime sets the maximum amount of time a lock can be held.
func WithMaxLockTime(maxLockTime time.Duration) Opt {
	return func(r *RLock) {
		r.maxLockTime = maxLockTime
	}
}

// WithRandomFunc sets the function used to generate random strings.
func WithRandomFunc(randomFunc func(int) string) Opt {
	return func(r *RLock) {
		r.randomFunc = randomFunc
	}
}

// WithLimit sets the concurrency limit for lock operations.
func WithLimit(limit int) Opt {
	return func(r *RLock) {
		r.limit = limit
	}
}

// WithLockID sets the identifier for the lock.
func WithLockID(lockID string) Opt {
	return func(r *RLock) {
		r.lockID = lockID
	}
}

// WithClockDrift sets the tolerance for clock drift between different nodes.
func WithClockDrift(drift float64) Opt {
	return func(r *RLock) {
		r.drift = drift
	}
}

// WithRetryTimes sets the number of times to retry acquiring the lock.
func WithRetryTimes(retry int) Opt {
	return func(r *RLock) {
		r.retry = retry
	}
}

// acquire attempts to acquire the lock using the SET NX command.
func acquire(ctx context.Context, cli *redis.Client, key string, value string, ttl time.Duration) error {
	boolCmd := cli.SetNX(ctx, key, value, ttl)
	if boolCmd.Err() != nil {
		return boolCmd.Err()
	}

	if !boolCmd.Val() {
		return fmt.Errorf("acquire lock %s failed", key)
	}
	return nil
}

// release attempts to release the lock by executing a Lua script to ensure it is released by the owner.
func release(ctx context.Context, cli *redis.Client, key string, value string) error {
	cmd := cli.Eval(ctx, luaScript, []string{key}, []string{value})
	if cmd.Err() != nil {
		return cmd.Err()
	}
	return nil
}

// acquireOrRelease abstracts the pattern of acquiring and releasing a lock, handling concurrency and errors.
func (r *RLock) acquireOrRelease(ctx context.Context, key, value string, ttl time.Duration, operation func(context.Context, *redis.Client, string, string, time.Duration) error) error {
	eg, newCtx := errgroup.WithContext(ctx)
	eg.SetLimit(r.limit)

	var (
		badCount  uint32
		goodCount uint32
		done      = make(chan struct{})
	)

	for _, client := range r.clients {
		client := client
		eg.Go(func() error {
			if err := operation(newCtx, client, key, value, ttl); err != nil {
				if atomic.AddUint32(&badCount, 1) >= r.critical {
					close(done)
					return fmt.Errorf("critical error occurred on client operation")
				}
			} else {
				if atomic.AddUint32(&goodCount, 1) >= r.critical {
					close(done)
					return nil
				}
			}
			return nil
		})
	}

	go func() {
		_ = eg.Wait()
	}()

	select {
	case <-done:
		if atomic.LoadUint32(&badCount) >= r.critical {
			return fmt.Errorf("critical error occurred on client operation")
		}
		if atomic.LoadUint32(&goodCount) >= r.critical {
			return nil
		}
		return fmt.Errorf("neither goodCount nor badCount reached the critical value")
	case <-newCtx.Done():
		return ctx.Err()
	}
}

var acquireFunc = func(ctx context.Context, client *redis.Client, key, value string, ttl time.Duration) error {
	return acquire(ctx, client, key, value, ttl)
}
var releaseFunc = func(ctx context.Context, client *redis.Client, key, value string, ttl time.Duration) error {
	return release(ctx, client, key, value)
}

// Lock attempts to acquire the lock and returns the remaining time the lock will be held.
func (r *RLock) Lock(ctx context.Context, key string, ttl time.Duration) (time.Duration, error) {
	value := fmt.Sprintf("%s:%s", r.lockID, r.randomFunc(20))
	clockDrift := time.Duration(int64(float64(ttl) * r.drift))
	startTime := time.Now()

	cancelCtx, cancel := context.WithTimeout(ctx, r.maxLockTime)
	defer cancel()

	// retry will be under the timeout context
	for i := 0; i < r.retry; i++ {
		if err := r.acquireOrRelease(cancelCtx, key, value, ttl, acquireFunc); err != nil {
			_ = r.acquireOrRelease(cancelCtx, key, value, ttl, releaseFunc)
			continue
		}
		left := ttl - time.Since(startTime) - clockDrift
		if left <= 0 {
			_ = r.acquireOrRelease(cancelCtx, key, value, ttl, releaseFunc)
			return 0, fmt.Errorf("lock %s timeout", key)
		}
		r.mu.Lock()
		r.m[key] = value
		r.mu.Unlock()
		return left, nil
	}
	return 0, errors.New("acquire lock failed")
}

// Unlock attempts to release the lock.
func (r *RLock) Unlock(ctx context.Context, key string) error {
	r.mu.Lock()
	value, ok := r.m[key]
	if !ok {
		r.mu.Unlock()
		return fmt.Errorf("lock %s not found", key)
	}
	delete(r.m, key)
	r.mu.Unlock()

	return r.acquireOrRelease(ctx, key, value, time.Duration(0), releaseFunc)
}

func (r *RLock) GetLockID() string {
	return r.lockID
}
