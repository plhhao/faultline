package fault

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/plhhao/faultline/internal/config"
)

func TestTruncateBoundaries(t *testing.T) {
	for _, source := range []string{"", "abcd"} {
		for _, limit := range []int{0, 2, 4, 9} {
			applied := 0
			b := WrapBody(context.Background(), io.NopCloser(strings.NewReader(source)), config.Fault{Action: "truncate", Bytes: &limit}, func() { applied++ })
			got, err := io.ReadAll(b)
			b.Close()
			cut := len(source) > limit
			if string(got) != source[:min(len(source), limit)] || errors.Is(err, ErrTruncated) != cut || (applied == 1) != cut {
				t.Fatalf("source=%q limit=%d got=%q err=%v applied=%d", source, limit, got, err, applied)
			}
		}
	}
}

type failingBody struct{}

func (failingBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (failingBody) Close() error             { return nil }
func TestTruncateNaturalError(t *testing.T) {
	for _, limit := range []int{0, 10} {
		b := WrapBody(context.Background(), failingBody{}, config.Fault{Action: "truncate", Bytes: &limit}, func() { t.Error("natural error recorded as applied") })
		if _, err := io.ReadAll(b); !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatal(err)
		}
	}
}

func TestThrottlePacingAndCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rate := 100
		started := time.Now()
		for range 2 {
			go func() {
				b := WrapBody(context.Background(), io.NopCloser(strings.NewReader(strings.Repeat("x", 100))), config.Fault{Action: "throttle", BytesPerSecond: &rate}, func() {})
				buffer := make([]byte, 100)
				for {
					n, err := b.Read(buffer)
					if n > 10 {
						t.Error("burst exceeded 100ms budget")
					}
					if err == io.EOF {
						break
					}
					if err != nil {
						t.Error(err)
						return
					}
				}
				if time.Since(started) != time.Second {
					t.Errorf("independent flow duration %s", time.Since(started))
				}
			}()
		}
		time.Sleep(time.Second)
		synctest.Wait()
		ctx, cancel := context.WithCancel(context.Background())
		reached := make(chan struct{})
		b := WrapBody(ctx, io.NopCloser(strings.NewReader("x")), config.Fault{Action: "throttle", BytesPerSecond: &rate}, func() { close(reached) })
		done := make(chan error, 1)
		go func() { _, err := io.ReadAll(b); done <- err }()
		<-reached
		cancel()
		synctest.Wait()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	})
}
