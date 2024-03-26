package rlock

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/go-redis/redismock/v9"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
)

// TestNewRLock tests the NewRLock function.
func TestNewRLock(t *testing.T) {
	clients := []*redis.Client{
		{},
		{},
	}
	// Test with an even number of clients.
	_, err := NewRLock(clients)
	assert.NotNil(t, err)
	// Test with an odd number of clients.
	clients = append(clients, &redis.Client{})
	lock, err := NewRLock(clients)
	assert.Nil(t, err)
	assert.NotNil(t, lock)
}

// TestAcquire tests the acquire function.
func TestAcquire(t *testing.T) {
	client, mock := redismock.NewClientMock()
	key := "test_lock"
	value := "lock_value"
	ttl := 10 * time.Second
	mock.ExpectSetNX(key, value, ttl).SetVal(true)
	err := acquire(context.Background(), client, key, value, ttl)
	assert.Nil(t, err)
}

// TestRelease tests the release function.
func TestRelease(t *testing.T) {
	client, mock := redismock.NewClientMock()
	key := "test_lock"
	value := "lock_value"
	ttl := 10 * time.Second
	mock.ExpectSetNX(key, value, ttl).SetVal(true)

	// acquire the lock first.
	err := acquire(context.Background(), client, key, value, ttl)
	assert.Nil(t, err)

	mock.ExpectEval(luaScript, []string{key}, []string{value}).SetVal(1)
	// release the lock.
	err = release(context.Background(), client, key, value)
	assert.Nil(t, err)
	mock.ExpectEval(luaScript, []string{key}, []string{"wrong_value"}).SetErr(errors.New("lock not owned"))
	// release again should fail because lock is not owned anymore.
	err = release(context.Background(), client, key, "wrong_value")
	assert.NotNil(t, err)
}

// TestLock tests the Lock method of
func TestLock(t *testing.T) {
	client, mock := redismock.NewClientMock()
	lockID := "test_lock_id"
	key := "test_lock"
	randomString := "random_string"
	ttl := 10 * time.Second
	mock.ExpectSetNX(key, fmt.Sprintf("%s:%s", lockID, randomString), ttl).SetVal(true)

	clients := []*redis.Client{
		client,
	}
	lock, _ := NewRLock(clients, WithLockID(lockID), WithRandomFunc(func(i int) string {
		return randomString
	}))

	// Attempt to acquire the lock.
	left, err := lock.Lock(context.Background(), key, ttl)
	assert.Nil(t, err)
	assert.Greater(t, left, 0*time.Millisecond)
}

// TestUnlock tests the Unlock method of
func TestUnlock(t *testing.T) {
	client, mock := redismock.NewClientMock()
	lockID := "test_lock_id"
	key := "test_lock"
	randomString := "random_string"
	ttl := 10 * time.Second
	mock.ExpectSetNX(key, fmt.Sprintf("%s:%s", lockID, randomString), ttl).SetVal(true)

	clients := []*redis.Client{
		client,
	}
	lock, _ := NewRLock(clients, WithLockID(lockID), WithRandomFunc(func(i int) string {
		return randomString
	}))

	// Attempt to acquire the lock.
	_, err := lock.Lock(context.Background(), key, ttl)
	assert.Nil(t, err)
	mock.ExpectEval(luaScript, []string{key}, []string{fmt.Sprintf("%s:%s", lockID, randomString)}).SetVal(1)

	// Attempt to release the lock.
	err = lock.Unlock(context.Background(), key)
	assert.Nil(t, err)
}

// TestWithLockID tests the WithLockID function.
func TestWithLockID(t *testing.T) {
	lockID := "test_lock_id"
	lock, err := NewRLock([]*redis.Client{{}}, WithLockID(lockID))
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, lockID, lock.GetLockID())
}

// TestWithLimit tests the WithLimit function.
func TestWithLimit(t *testing.T) {
	limit := 10
	lock, err := NewRLock([]*redis.Client{{}}, WithLimit(limit))
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, limit, lock.limit)
}

// TestWithRandomFunc tests the WithRandomFunc function.
func TestWithRandomFunc(t *testing.T) {
	var randomString = "random_string"
	randomFunc := func(i int) string {
		return randomString
	}
	lock, err := NewRLock([]*redis.Client{{}}, WithRandomFunc(randomFunc))
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, randomString, lock.randomFunc(10))
}

// TestWithDrift tests the WithDrift function.
func TestWithDrift(t *testing.T) {
	drift := 0.1
	lock, err := NewRLock([]*redis.Client{{}}, WithClockDrift(drift))
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, drift, lock.drift)
}

// TestWithMaxLockTime tests the WithMaxLockTime function.
func TestWithMaxLockTime(t *testing.T) {
	maxLockTime := 10 * time.Second
	lock, err := NewRLock([]*redis.Client{{}}, WithMaxLockTime(maxLockTime))
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, maxLockTime, lock.maxLockTime)
}

// TestWithRetryTimes tests the WithRetryTimes function.
func TestWithRetryTimes(t *testing.T) {
	retry := 10
	lock, err := NewRLock([]*redis.Client{{}}, WithRetryTimes(retry))
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, retry, lock.retry)
}
