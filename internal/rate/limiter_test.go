package rate

import (
	"testing"
	"time"
)

func TestAllow(t *testing.T) {
	l := New(5, 100*time.Millisecond)

	// First 5 should be allowed.
	for i := 0; i < 5; i++ {
		if !l.Allow("test") {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}

	// 6th should be blocked.
	if l.Allow("test") {
		t.Fatal("6th request should be blocked")
	}
}

func TestAllowN(t *testing.T) {
	l := New(10, time.Second)

	if !l.AllowN("big", 8) {
		t.Fatal("should allow 8 tokens")
	}
	if l.AllowN("big", 5) {
		t.Fatal("should block 5 tokens (only 2 remaining)")
	}
}

func TestRefill(t *testing.T) {
	l := New(2, 50*time.Millisecond)

	if !l.Allow("refill") {
		t.Fatal("first request should be allowed")
	}
	if !l.Allow("refill") {
		t.Fatal("second request should be allowed")
	}
	if l.Allow("refill") {
		t.Fatal("third request should be blocked")
	}

	time.Sleep(60 * time.Millisecond) // wait for ~1 token refill

	if !l.Allow("refill") {
		t.Fatal("after refill, request should be allowed")
	}
}

func TestSeparateKeys(t *testing.T) {
	l := New(2, time.Second)

	if !l.Allow("alpha") {
		t.Fatal("alpha 1 should be allowed")
	}
	if !l.Allow("beta") {
		t.Fatal("beta 1 should be allowed")
	}
	if !l.Allow("alpha") {
		t.Fatal("alpha 2 should be allowed")
	}
	if !l.Allow("beta") {
		t.Fatal("beta 2 should be allowed")
	}

	// Both exhausted.
	if l.Allow("alpha") {
		t.Fatal("alpha 3 should be blocked")
	}
	if l.Allow("beta") {
		t.Fatal("beta 3 should be blocked")
	}
}

func TestRemaining(t *testing.T) {
	l := New(3, time.Second)

	if rem := l.Remaining("test"); rem != 3 {
		t.Errorf("initial remaining = %f, want 3", rem)
	}

	l.Allow("test")
	l.Allow("test")

	// Allow exactly 2 tokens consumed, but refill may have added a tiny epsilon.
	rem := l.Remaining("test")
	if rem < 0.99 || rem > 1.1 {
		t.Errorf("remaining = %f, want ~1", rem)
	}
}

func TestReset(t *testing.T) {
	l := New(3, time.Second)
	l.Allow("a")
	l.Allow("a")
	l.Allow("a")

	if l.Allow("a") {
		t.Fatal("should be blocked")
	}

	l.Reset()

	if !l.Allow("a") {
		t.Fatal("after reset should be allowed")
	}
}

func TestConcurrentAccess(t *testing.T) {
	l := New(100, time.Second)

	done := make(chan struct{})
	for g := 0; g < 10; g++ {
		go func() {
			for i := 0; i < 10; i++ {
				l.Allow("conc")
			}
			done <- struct{}{}
		}()
	}

	for g := 0; g < 10; g++ {
		<-done
	}
	// If we reach here, no race condition.
}
