package agent

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestUploadsConcurrentStartFinishAndDiscard(t *testing.T) {
	t.Run("distinct paths", func(t *testing.T) {
		dir := t.TempDir()
		roots := Roots{Read: []string{dir}, Write: []string{dir}}
		u := NewUploads(16)
		t.Cleanup(func() { _ = u.Close() })

		const workers = 8
		const iters = 12

		var wg sync.WaitGroup
		errCh := make(chan error, workers*iters)
		for w := 0; w < workers; w++ {
			w := w
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < iters; i++ {
					dest := filepath.Join(dir, fmt.Sprintf("file-w%d-i%d.bin", w, i))
					payload := []byte(fmt.Sprintf("payload-worker-%d-iter-%d", w, i))
					up, err := u.Start(roots, dest, int64(len(payload)), int64(1000+i), "", false)
					if err != nil {
						errCh <- fmt.Errorf("w=%d i=%d start: %w", w, i, err)
						return
					}
					for off := 0; off < len(payload); off += 16 {
						end := off + 16
						if end > len(payload) {
							end = len(payload)
						}
						chunk := payload[off:end]
						if _, err := u.Put(up.ID, int64(off), chunk, sha256Hex(chunk)); err != nil {
							errCh <- fmt.Errorf("w=%d i=%d put: %w", w, i, err)
							return
						}
					}
					fin, err := u.Finish(up.ID, sha256Hex(payload))
					if err != nil {
						errCh <- fmt.Errorf("w=%d i=%d finish: %w", w, i, err)
						return
					}
					if !fin.Complete {
						errCh <- fmt.Errorf("w=%d i=%d expected Complete=true", w, i)
						return
					}
					got, err := os.ReadFile(dest)
					if err != nil || !bytes.Equal(got, payload) {
						errCh <- fmt.Errorf("w=%d i=%d content=%q err=%v", w, i, got, err)
						return
					}
				}
			}()
		}
		wg.Wait()
		close(errCh)
		for err := range errCh {
			t.Error(err)
		}
	})

	t.Run("same destination stress", func(t *testing.T) {
		dir := t.TempDir()
		roots := Roots{Read: []string{dir}, Write: []string{dir}}
		u := NewUploads(16)
		t.Cleanup(func() { _ = u.Close() })

		dest := filepath.Join(dir, "shared.bin")
		const workers = 6
		const iters = 10

		var wg sync.WaitGroup
		for w := 0; w < workers; w++ {
			w := w
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < iters; i++ {
					payload := []byte(fmt.Sprintf("shared-w%d-i%d-%s", w, i, strings.Repeat("x", (w+i)%7+1)))
					mtime := int64(w*100 + i + 1)
					up, err := u.Start(roots, dest, int64(len(payload)), mtime, "", true)
					if err != nil {
						continue
					}
					_, _ = u.Status(up.ID)
					for off := 0; off < len(payload); off += 16 {
						end := off + 16
						if end > len(payload) {
							end = len(payload)
						}
						chunk := payload[off:end]
						if _, err := u.Put(up.ID, int64(off), chunk, sha256Hex(chunk)); err != nil {
							break
						}
					}
					switch (w + i) % 3 {
					case 0:
						_, _ = u.Finish(up.ID, sha256Hex(payload))
					case 1:
						_, _ = u.Finish(up.ID, strings.Repeat("00", 32))
					default:
						_ = u.Abort(up.ID)
					}
				}
			}()
		}
		wg.Wait()
	})
}
