package cache

import (
	"testing"
	"time"
)

func TestSetAndGet(t *testing.T) {
	c := New(1*time.Minute, 0)
	defer c.Stop()

	c.Set("key1", "value1")
	val, ok := c.Get("key1")
	if !ok {
		t.Fatal("expected key to exist")
	}
	if val.(string) != "value1" {
		t.Errorf("got %v, want value1", val)
	}
}

func TestGetMissing(t *testing.T) {
	c := New(1*time.Minute, 0)
	defer c.Stop()

	_, ok := c.Get("nonexistent")
	if ok {
		t.Fatal("expected key to be missing")
	}
}

func TestExpiration(t *testing.T) {
	c := New(50*time.Millisecond, 0)
	defer c.Stop()

	c.Set("key", "value")

	// Should still be there immediately.
	_, ok := c.Get("key")
	if !ok {
		t.Fatal("key should exist before TTL")
	}

	time.Sleep(100 * time.Millisecond)

	_, ok = c.Get("key")
	if ok {
		t.Fatal("key should have expired")
	}
}

func TestSetWithTTL(t *testing.T) {
	c := New(1*time.Minute, 0)
	defer c.Stop()

	c.SetWithTTL("quick", "gone", 30*time.Millisecond)
	_, ok := c.Get("quick")
	if !ok {
		t.Fatal("key should exist immediately")
	}

	time.Sleep(50 * time.Millisecond)

	_, ok = c.Get("quick")
	if ok {
		t.Fatal("key should have expired after custom TTL")
	}
}

func TestDelete(t *testing.T) {
	c := New(1*time.Minute, 0)
	defer c.Stop()

	c.Set("key", "value")
	c.Delete("key")

	_, ok := c.Get("key")
	if ok {
		t.Fatal("key should be deleted")
	}
}

func TestClear(t *testing.T) {
	c := New(1*time.Minute, 0)
	defer c.Stop()

	c.Set("a", 1)
	c.Set("b", 2)
	c.Clear()

	_, ok := c.Get("a")
	if ok {
		t.Fatal("key should be cleared")
	}
	_, ok = c.Get("b")
	if ok {
		t.Fatal("key should be cleared")
	}
}

func TestConcurrentAccess(t *testing.T) {
	c := New(1*time.Minute, 0)
	defer c.Stop()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			c.Set("key", i)
		}
		done <- struct{}{}
	}()
	go func() {
		for i := 0; i < 100; i++ {
			c.Get("key")
		}
		done <- struct{}{}
	}()
	go func() {
		for i := 0; i < 100; i++ {
			c.Delete("key")
		}
		done <- struct{}{}
	}()

	<-done
	<-done
	<-done
	// If we reach this, no race condition occurred.
}

func TestCleanup(t *testing.T) {
	c := New(30*time.Millisecond, 10*time.Millisecond)
	defer c.Stop()

	c.Set("expire", "soon")
	time.Sleep(100 * time.Millisecond)

	// The cleanup goroutine should have removed it.
	c.mu.RLock()
	_, exists := c.items["expire"]
	c.mu.RUnlock()
	if exists {
		t.Fatal("cleanup should have removed expired item")
	}
}
