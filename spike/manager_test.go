package spike

import (
	"context"
	"math/big"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gocache "github.com/patrickmn/go-cache"
	"github.com/stretchr/testify/assert"
)

func TestManager(t *testing.T) {
	tokens := []string{"1", "2", "3", "4", "1", "2", "3", "4"}
	response := map[string]*big.Int{
		"1": big.NewInt(9031161740652627),
		"2": big.NewInt(336199114644976),
		"3": big.NewInt(336578093626181),
		"4": big.NewInt(10),
	}
	cacheControl := new(int32)
	m := NewManager(func(ctx context.Context, k string) (*big.Int, error) {
		atomic.AddInt32(cacheControl, 1)
		return response[k], nil
	}, time.Second*3)

	wg := sync.WaitGroup{}
	wg.Add(len(tokens) * 11)
	for i := 0; i <= 10; i++ {
		for _, token := range tokens {
			go func(token string) {
				defer wg.Done()
				res, err := m.GetResult(context.Background(), token)

				assert.NoError(t, err)
				assert.Equal(t, res, response[token])
			}(token)
		}
		<-time.After(time.Millisecond * 100)
	}
	wg.Wait()
	assert.Equal(t, int(atomic.LoadInt32(cacheControl)), 4)
	<-time.After(time.Second * 3)

	atomic.StoreInt32(cacheControl, 0)
	wg.Add(len(tokens) * 11)
	for i := 0; i <= 10; i++ {
		for _, token := range tokens {
			go func(token string) {
				defer wg.Done()
				res, err := m.GetResult(context.Background(), token)

				assert.NoError(t, err)
				assert.Equal(t, res, response[token])
			}(token)
		}
		<-time.After(time.Millisecond * 100)
	}
	wg.Wait()
	assert.Equal(t, int(atomic.LoadInt32(cacheControl)), 4)
}

func TestCustomManager(t *testing.T) {
	tokens := []string{"1", "2", "3", "4", "1", "2", "3", "4"}
	response := map[string]*big.Int{
		"1": big.NewInt(9031161740652627),
		"2": big.NewInt(336199114644976),
		"3": big.NewInt(336578093626181),
		"4": big.NewInt(10),
	}

	g := gocache.New(gocache.NoExpiration, gocache.DefaultExpiration)
	cacheControl := new(int32)
	handler := Handler[*big.Int]{
		Fetch: func(ctx context.Context, t string) (*big.Int, error) {
			atomic.AddInt32(cacheControl, 1)
			return response[t], nil
		},
		Set: func(k string, v *big.Int) {
			g.Set(k, v, time.Second*2)
		},
		Get: func(k string) (*big.Int, bool) {
			v, ok := g.Get(k)
			if !ok {
				return nil, false
			}
			return v.(*big.Int), true
		},
	}

	manager := NewCustomManager(handler)
	wg := sync.WaitGroup{}
	wg.Add(len(tokens) * 11)
	for i := 0; i <= 10; i++ {
		for _, token := range tokens {
			go func(token string) {
				defer wg.Done()
				res, err := manager.GetResult(context.Background(), token)
				assert.NoError(t, err)
				assert.Equal(t, res, response[token])
			}(token)
		}
		<-time.After(time.Millisecond * 100)
	}
	wg.Wait()
	assert.Equal(t, int(atomic.LoadInt32(cacheControl)), 4)
	<-time.After(time.Second * 2)

	_, ok := g.Get(tokens[0])
	assert.Equal(t, ok, false)

	atomic.StoreInt32(cacheControl, 0)
	wg.Add(len(tokens) * 11)
	for i := 0; i <= 10; i++ {
		for _, token := range tokens {
			go func(token string) {
				defer wg.Done()
				res, err := manager.GetResult(context.Background(), token)
				assert.NoError(t, err)
				assert.Equal(t, res, response[token])
			}(token)
		}
		<-time.After(time.Millisecond * 100)
	}
	wg.Wait()
	assert.Equal(t, int(atomic.LoadInt32(cacheControl)), 4)
}
