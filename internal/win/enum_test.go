package win

import (
	"sync"
	"testing"
	"time"
)

// enumAll runs at once from several requests since lanes (multi-agent
// 06): each enumeration must get its own windows, and none may see
// another's end. Before the fix a callback appended through a target
// another call had already cleared, panicked holding the lock, and every
// later Collect blocked. EnumWindows only reads the window list.
func TestEnumAllIsSafeConcurrently(t *testing.T) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		var wg sync.WaitGroup
		for range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for range 50 {
					hwnds, err := enumAll()
					if err != nil {
						t.Errorf("enumAll: %v", err)
						return
					}
					if len(hwnds) == 0 {
						t.Error("enumAll found no top-level window")
						return
					}
				}
			}()
		}
		wg.Wait()
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("concurrent enumAll calls deadlocked")
	}
}
