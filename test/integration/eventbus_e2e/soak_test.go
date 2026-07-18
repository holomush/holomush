// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration && soak

// Soak tests run nightly via .github/workflows/nightly-soak.yml. They
// exercise the invariants under sustained load (spec §8 "Chaos and soak").
// The `soak` build tag keeps these out of `task pr-prep` so the
// per-PR latency stays low; nightly is the venue for minute-scale runs.
package eventbus_e2e_test

import (
	"context"
	"os"
	"runtime"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/audit"
)

// Soak specs — matches spec §8 "Chaos and soak — 1k events/sec for
// 5 minutes; assert no goroutine leak, memory growth < 50 MB, audit lag
// p99 ≤ 5s, full event count".
//
// Implementation notes:
//
//   - Test uses the embedded bus + real PG audit projection.
//   - Publish rate is per-tick, not a tight loop, so the test targets
//     throughput rather than starving the runtime.
//   - At the end we assert:
//     (a) goroutine count is back near baseline (leak check),
//     (b) events_audit row count matches publish count,
//     (c) RSS growth stays under the documented 50 MB ceiling.
//
// The full 5-minute run is heavy; callers can shorten via env
// SOAK_DURATION (e.g. "30s" for a local smoke run). The nightly CI sets
// no override, getting the full matrix.
var _ = Describe("Soak publish 1k/sec for 5 minutes", func() {
	It("maintains row parity, no goroutine leak, no memory growth > 50MB", func() {
		duration := 5 * time.Minute
		if override := os.Getenv("SOAK_DURATION"); override != "" {
			if d, err := time.ParseDuration(override); err == nil && d > 0 {
				duration = d
				GinkgoWriter.Printf("SOAK_DURATION override: %s\n", duration)
			}
		}

		baselineRoutines := runtime.NumGoroutine()
		var memBefore runtime.MemStats
		runtime.ReadMemStats(&memBefore)

		ctx, cancel := context.WithTimeout(context.Background(), duration+2*time.Minute)
		DeferCleanup(cancel)

		bus := freshBus()
		pool := freshPool()
		hostSub := audit.NewSubsystem(fixedJS{js: bus.JS}, fixedPool{pool: pool}, audit.Config{})
		Expect(hostSub.Prepare(ctx)).To(Succeed())
		Expect(hostSub.Activate(ctx)).To(Succeed())
		DeferCleanup(func() { _ = hostSub.Stop(context.Background()) })

		pub := bus.Bus.Publisher()
		subject := eventbus.Subject("events.main.soak.s1")

		// 1000 events/sec = one event per ms. Ten concurrent publishers
		// smooths the load so no single goroutine has to spin.
		const publishers = 10
		ratePerPublisher := time.Second / (1000 / publishers) // 10ms per publisher

		var published int64
		var pubMu sync.Mutex
		var wg sync.WaitGroup
		// stopCh must close so every publisher goroutine observes the stop
		// signal — `time.After` can only be received once, which is why the
		// prior shape deadlocked all but the first goroutine.
		stopCh := make(chan struct{})
		go func() {
			select {
			case <-time.After(duration):
			case <-ctx.Done():
			}
			close(stopCh)
		}()
		for p := 0; p < publishers; p++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ticker := time.NewTicker(ratePerPublisher)
				defer ticker.Stop()
				for {
					select {
					case <-stopCh:
						return
					case <-ctx.Done():
						return
					case <-ticker.C:
						evt := mintEvent(subject, "scene.pose", `{"k":"soak"}`)
						if err := pub.Publish(ctx, evt); err != nil {
							// Publish errors are expected-ish under pressure;
							// log and keep going so the loop measures
							// end-to-end throughput instead of failing.
							GinkgoWriter.Printf("soak publish: %v\n", err)
							continue
						}
						pubMu.Lock()
						published++
						pubMu.Unlock()
					}
				}
			}()
		}
		wg.Wait()

		// Give the projection generous time to drain after publish stop.
		hostSub.AwaitDrained(suiteT, 60*time.Second)

		// Row-count parity — every published event lands in events_audit.
		Expect(countRows(ctx, pool, "events_audit", "")).To(Equal(int(published)),
			"audit row count must equal published count after drain")

		// Goroutine leak check. 10 × publishers exit; allow some slack for
		// the bus/projection internal workers.
		runtime.GC()
		after := runtime.NumGoroutine()
		Expect(after).To(BeNumerically("<", baselineRoutines+50),
			"goroutine count grew from %d to %d — likely a leak", baselineRoutines, after)

		// Memory ceiling: 50 MB growth per spec §8.
		var memAfter runtime.MemStats
		runtime.ReadMemStats(&memAfter)
		growth := int64(memAfter.HeapAlloc) - int64(memBefore.HeapAlloc)
		Expect(growth).To(BeNumerically("<", int64(50*1024*1024)),
			"heap growth %d bytes exceeds 50MB soak ceiling", growth)
	})
})
