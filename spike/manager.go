// Package spike provides a primitive to handle spike-like load on retrieving external resources
package spike

import (
	"context"
	"sync"
	"time"

	gocache "github.com/patrickmn/go-cache"
)

const (
	taskQueueLen           = 60
	currentlyExecutedSize  = 50
	defaultCleanupInterval = 5 * time.Millisecond
)

// TODO: cache errors and allow common.Hash as key

type Manager[T any] struct {
	mu                sync.RWMutex
	handler           Handler[T]
	taskQueue         chan task[T]
	currentlyExecuted map[string][]chan<- result[T]
}

// NewCustomManager creates a new Manager with a custom cache implementation controlled by client code
// it should be used for non-trivial flows or non-default cache implementations
func NewCustomManager[T any](h Handler[T]) *Manager[T] {
	cm := &Manager[T]{
		handler:           h,
		taskQueue:         make(chan task[T], taskQueueLen),
		currentlyExecuted: make(map[string][]chan<- result[T], currentlyExecutedSize),
	}
	go cm.start()
	return cm
}

// NewManager creates a new Manager with a default cache implementation
// it is preferred way of creating a new Manager
func NewManager[T any](fetch func(ctx context.Context, k string) (T, error), cacheTime time.Duration) *Manager[T] {
	g := gocache.New(cacheTime, defaultCleanupInterval)
	return NewCustomManager[T](Handler[T]{
		Fetch: fetch,
		Set: func(k string, v T) {
			g.Set(k, v, cacheTime)
		},
		Get: func(k string) (T, bool) {
			v, ok := g.Get(k)
			if !ok {
				var rt T
				return rt, false
			}
			//nolint:forcetypeassert
			return v.(T), true
		},
	})
}

type Handler[T any] struct {
	Fetch func(ctx context.Context, k string) (T, error)
	Set   func(k string, v T)
	Get   func(k string) (T, bool)
}

type task[T any] struct {
	key string
	res chan<- result[T]
}

type result[T any] struct {
	v T
	e error
}

func (m *Manager[T]) start() {
	for t := range m.taskQueue {
		m.mu.Lock()
		v, ok := m.handler.Get(t.key)
		if ok {
			t.res <- result[T]{v: v}
			close(t.res)
			m.mu.Unlock()
			continue
		}

		chans, ok := m.currentlyExecuted[t.key]
		if ok {
			chans = append(chans, t.res)
			m.currentlyExecuted[t.key] = chans
			m.mu.Unlock()
			continue
		}
		m.mu.Unlock()

		go func(currentTask task[T]) {
			m.mu.Lock()
			v, ok := m.handler.Get(currentTask.key)
			if ok {
				currentTask.res <- result[T]{v: v}
				close(currentTask.res)
				m.mu.Unlock()
				return
			}
			chans, ok := m.currentlyExecuted[currentTask.key]
			if ok {
				chans = append(chans, currentTask.res)
				m.currentlyExecuted[currentTask.key] = chans
				m.mu.Unlock()
				return
			}

			m.currentlyExecuted[currentTask.key] = []chan<- result[T]{currentTask.res}
			m.mu.Unlock()

			res, err := m.handler.Fetch(context.Background(), currentTask.key)
			if err != nil {
				m.mu.Lock()
				chans = m.currentlyExecuted[currentTask.key]
				for _, ch := range chans {
					ch <- result[T]{e: err}
					close(ch)
				}

				delete(m.currentlyExecuted, currentTask.key)
				m.mu.Unlock()
				return
			}
			m.handler.Set(currentTask.key, res)

			m.mu.Lock()
			chans = m.currentlyExecuted[currentTask.key]
			for _, ch := range chans {
				ch <- result[T]{v: res}
				close(ch)
			}
			delete(m.currentlyExecuted, currentTask.key)
			m.mu.Unlock()
		}(t)
	}
}

func (m *Manager[T]) GetResult(ctx context.Context, k string) (T, error) { //nolint:ireturn
	r, ok := m.handler.Get(k)
	if ok {
		return r, nil
	}

	resChan := make(chan result[T], 1)

	t := task[T]{
		key: k,
		res: resChan,
	}
	m.taskQueue <- t
	select {
	case <-ctx.Done():
		var tr T
		return tr, ctx.Err()
	case completed := <-resChan:
		return completed.v, completed.e
	}
}
