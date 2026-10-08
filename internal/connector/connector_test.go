package connector

import (
	"sync"
	"testing"
)

func TestTunnelCloseIsIdempotent(t *testing.T) {
	calls := 0
	tun := NewTunnel(func() { calls++ })

	select {
	case <-tun.Done():
		t.Fatal("Done must be open before Close")
	default:
	}

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); tun.Close() }()
	}
	wg.Wait()

	if calls != 1 {
		t.Errorf("close func called %d times, want 1", calls)
	}
	<-tun.Done()
}

func TestMarkDoneBeforeClose(t *testing.T) {
	tun := NewTunnel(nil)
	tun.MarkDone() // forwarder died on its own
	tun.MarkDone()
	<-tun.Done()
	tun.Close() // must not panic on an already-closed Done
}
