// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package access_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/stretchr/testify/mock"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/internal/world/worldtest"
)

// transientErrorEngine fails for the first N calls, then allows all requests.
// Safe for concurrent use.
type transientErrorEngine struct {
	mu        sync.Mutex
	callCount int
	failUntil int
	failErr   error
}

func newTransientErrorEngine(failUntil int, failErr error) *transientErrorEngine {
	return &transientErrorEngine{
		failUntil: failUntil,
		failErr:   failErr,
	}
}

func (e *transientErrorEngine) Evaluate(_ context.Context, _ types.AccessRequest) (types.Decision, error) {
	e.mu.Lock()
	e.callCount++
	count := e.callCount
	e.mu.Unlock()

	if count <= e.failUntil {
		return types.Decision{}, e.failErr
	}
	return types.NewDecision(types.EffectAllow, "recovered", "test-policy"), nil
}

func (e *transientErrorEngine) CallCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.callCount
}

// perCallEngine invokes a user-supplied function for each Evaluate call,
// passing the 1-based call ordinal. Safe for concurrent use.
type perCallEngine struct {
	mu        sync.Mutex
	callCount int
	fn        func(callNum int) (types.Decision, error)
}

func newPerCallEngine(fn func(callNum int) (types.Decision, error)) *perCallEngine {
	return &perCallEngine{fn: fn}
}

func (e *perCallEngine) Evaluate(_ context.Context, _ types.AccessRequest) (types.Decision, error) {
	e.mu.Lock()
	e.callCount++
	n := e.callCount
	e.mu.Unlock()
	return e.fn(n)
}

// alwaysErrorEngine returns the configured error on every call. Safe for concurrent use.
type alwaysErrorEngine struct {
	err error
}

func (e *alwaysErrorEngine) Evaluate(_ context.Context, _ types.AccessRequest) (types.Decision, error) {
	return types.Decision{}, e.err
}

var _ = Describe("Concurrent engine failures and recovery", func() {
	const goroutines = 50

	var (
		ctx       context.Context
		subjectID string
		locID     ulid.ULID
	)

	BeforeEach(func() {
		ctx = context.Background()
		subjectID = access.CharacterSubject(ulid.Make().String())
		locID = ulid.Make()
	})

	Describe("Concurrent evaluate failures", func() {
		It("returns ErrAccessEvaluationFailed for every concurrent caller", func() {
			engine := &alwaysErrorEngine{err: errors.New("engine down")}
			svc := world.NewService(world.ServiceConfig{
				LocationRepo: worldtest.NewMockLocationRepository(GinkgoT()),
				Engine:       engine,
			})

			var wg sync.WaitGroup
			errs := make([]error, goroutines)

			for i := range goroutines {
				wg.Add(1)
				go func(idx int) {
					defer GinkgoRecover()
					defer wg.Done()
					_, err := svc.GetLocation(ctx, subjectID, locID)
					errs[idx] = err
				}(i)
			}
			wg.Wait()

			for i, err := range errs {
				Expect(err).To(HaveOccurred(), fmt.Sprintf("goroutine %d should have failed", i))
				Expect(errors.Is(err, world.ErrAccessEvaluationFailed)).To(BeTrue(),
					fmt.Sprintf("goroutine %d: expected ErrAccessEvaluationFailed, got %v", i, err))
			}
		})

		It("produces no cross-contamination between concurrent errors", func() {
			engine := newPerCallEngine(func(callNum int) (types.Decision, error) {
				return types.Decision{}, fmt.Errorf("failure-%d", callNum)
			})
			svc := world.NewService(world.ServiceConfig{
				LocationRepo: worldtest.NewMockLocationRepository(GinkgoT()),
				Engine:       engine,
			})

			var wg sync.WaitGroup
			errs := make([]error, goroutines)

			for i := range goroutines {
				wg.Add(1)
				go func(idx int) {
					defer GinkgoRecover()
					defer wg.Done()
					_, err := svc.GetLocation(ctx, subjectID, locID)
					errs[idx] = err
				}(i)
			}
			wg.Wait()

			// Every error must be distinct (wrapping a unique "failure-N" message)
			// and all must wrap ErrAccessEvaluationFailed.
			seen := make(map[string]bool, goroutines)
			for i, err := range errs {
				Expect(err).To(HaveOccurred(), fmt.Sprintf("goroutine %d should have failed", i))
				Expect(errors.Is(err, world.ErrAccessEvaluationFailed)).To(BeTrue(),
					fmt.Sprintf("goroutine %d: expected ErrAccessEvaluationFailed", i))
				msg := err.Error()
				Expect(seen[msg]).To(BeFalse(),
					fmt.Sprintf("goroutine %d: duplicate error message %q indicates cross-contamination", i, msg))
				seen[msg] = true
			}
		})

		It("does not panic under concurrent engine errors across different entity types", func() {
			engine := &alwaysErrorEngine{err: errors.New("engine down")}
			mockLocRepo := worldtest.NewMockLocationRepository(GinkgoT())
			mockExitRepo := worldtest.NewMockExitRepository(GinkgoT())
			mockObjRepo := worldtest.NewMockObjectRepository(GinkgoT())

			svc := world.NewService(world.ServiceConfig{
				LocationRepo: mockLocRepo,
				ExitRepo:     mockExitRepo,
				ObjectRepo:   mockObjRepo,
				Engine:       engine,
			})

			var wg sync.WaitGroup
			errs := make([]error, goroutines*3)

			// GetLocation calls
			for i := range goroutines {
				wg.Add(1)
				go func(idx int) {
					defer GinkgoRecover()
					defer wg.Done()
					_, err := svc.GetLocation(ctx, subjectID, ulid.Make())
					errs[idx] = err
				}(i)
			}
			// GetExit calls
			for i := range goroutines {
				wg.Add(1)
				go func(idx int) {
					defer GinkgoRecover()
					defer wg.Done()
					_, err := svc.GetExit(ctx, subjectID, ulid.Make())
					errs[goroutines+idx] = err
				}(i)
			}
			// GetObject calls
			for i := range goroutines {
				wg.Add(1)
				go func(idx int) {
					defer GinkgoRecover()
					defer wg.Done()
					_, err := svc.GetObject(ctx, subjectID, ulid.Make())
					errs[2*goroutines+idx] = err
				}(i)
			}
			wg.Wait()

			for i, err := range errs {
				Expect(err).To(HaveOccurred(), fmt.Sprintf("call %d should have failed", i))
				Expect(errors.Is(err, world.ErrAccessEvaluationFailed)).To(BeTrue(),
					fmt.Sprintf("call %d: expected ErrAccessEvaluationFailed, got %v", i, err))
			}
		})
	})

	Describe("Engine recovery after transient failures", func() {
		It("fails initially then succeeds after recovery", func() {
			failCount := 10
			engine := newTransientErrorEngine(failCount, errors.New("transient failure"))

			expectedLoc := &world.Location{ID: locID, Name: "Test Room"}
			mockRepo := worldtest.NewMockLocationRepository(GinkgoT())
			mockRepo.On("Get", mock.Anything, mock.Anything).Maybe().Return(expectedLoc, nil)

			svc := world.NewService(world.ServiceConfig{
				LocationRepo: mockRepo,
				Engine:       engine,
			})

			// First failCount calls should fail
			for i := range failCount {
				_, err := svc.GetLocation(ctx, subjectID, locID)
				Expect(err).To(HaveOccurred(), fmt.Sprintf("call %d should fail", i+1))
				Expect(errors.Is(err, world.ErrAccessEvaluationFailed)).To(BeTrue())
			}

			// Subsequent calls should succeed
			for i := range 10 {
				loc, err := svc.GetLocation(ctx, subjectID, locID)
				Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("recovery call %d should succeed", i+1))
				Expect(loc).To(Equal(expectedLoc))
			}

			Expect(engine.CallCount()).To(Equal(failCount + 10))
		})

		It("does not enter a degraded state after concurrent failures resolve", func() {
			// Engine fails for first 20 calls, then recovers.
			// Fire 30 goroutines: some will fail, some will succeed.
			// After all complete, verify the engine is healthy.
			failCount := 20
			totalCalls := 30
			engine := newTransientErrorEngine(failCount, errors.New("transient"))

			expectedLoc := &world.Location{ID: locID, Name: "Recovered Room"}
			mockRepo := worldtest.NewMockLocationRepository(GinkgoT())
			mockRepo.On("Get", mock.Anything, mock.Anything).Maybe().Return(expectedLoc, nil)

			svc := world.NewService(world.ServiceConfig{
				LocationRepo: mockRepo,
				Engine:       engine,
			})

			var wg sync.WaitGroup
			var failureCount atomic.Int32
			var successCount atomic.Int32

			for range totalCalls {
				wg.Add(1)
				go func() {
					defer GinkgoRecover()
					defer wg.Done()
					_, err := svc.GetLocation(ctx, subjectID, locID)
					if err != nil {
						failureCount.Add(1)
					} else {
						successCount.Add(1)
					}
				}()
			}
			wg.Wait()

			// Some calls should have failed, some succeeded
			Expect(failureCount.Load()).To(BeNumerically(">", 0), "expected some failures during transient period")
			Expect(successCount.Load()).To(BeNumerically(">", 0), "expected some successes after recovery")
			Expect(int(failureCount.Load() + successCount.Load())).To(Equal(totalCalls))

			// Post-recovery: all subsequent calls should succeed
			for i := range 20 {
				loc, err := svc.GetLocation(ctx, subjectID, locID)
				Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("post-recovery call %d should succeed", i+1))
				Expect(loc).To(Equal(expectedLoc))
			}
		})
	})

	Describe("Mixed concurrent operations — some succeed, some fail", func() {
		It("routes success and failure correctly to each caller", func() {
			// Odd-numbered calls fail, even-numbered calls succeed.
			engine := newPerCallEngine(func(callNum int) (types.Decision, error) {
				if callNum%2 == 1 {
					return types.Decision{}, fmt.Errorf("odd-call-failure-%d", callNum)
				}
				return types.NewDecision(types.EffectAllow, "even-ok", "test-policy"), nil
			})

			expectedLoc := &world.Location{ID: locID, Name: "Mixed Room"}
			mockRepo := worldtest.NewMockLocationRepository(GinkgoT())
			mockRepo.On("Get", mock.Anything, mock.Anything).Maybe().Return(expectedLoc, nil)

			svc := world.NewService(world.ServiceConfig{
				LocationRepo: mockRepo,
				Engine:       engine,
			})

			type result struct {
				loc *world.Location
				err error
			}

			var wg sync.WaitGroup
			results := make([]result, goroutines)

			for i := range goroutines {
				wg.Add(1)
				go func(idx int) {
					defer GinkgoRecover()
					defer wg.Done()
					loc, err := svc.GetLocation(ctx, subjectID, locID)
					results[idx] = result{loc: loc, err: err}
				}(i)
			}
			wg.Wait()

			var successes, failures int
			for _, r := range results {
				if r.err != nil {
					failures++
					Expect(errors.Is(r.err, world.ErrAccessEvaluationFailed)).To(BeTrue(),
						fmt.Sprintf("failure should be ErrAccessEvaluationFailed, got %v", r.err))
					Expect(r.loc).To(BeNil())
				} else {
					successes++
					Expect(r.loc).To(Equal(expectedLoc))
				}
			}

			// With 50 goroutines and odd/even split, we expect roughly half each.
			// Use generous bounds to avoid flakiness.
			Expect(successes).To(BeNumerically(">", 0), "expected some operations to succeed")
			Expect(failures).To(BeNumerically(">", 0), "expected some operations to fail")
			Expect(successes + failures).To(Equal(goroutines))
		})

		It("handles mixed entity types correctly under concurrent succeed/fail", func() {
			// Use a shared perCallEngine so each entity type gets its own
			// mix of successes and failures based on call ordering.
			engine := newPerCallEngine(func(callNum int) (types.Decision, error) {
				if callNum%3 == 0 {
					return types.Decision{}, fmt.Errorf("every-third-failure-%d", callNum)
				}
				return types.NewDecision(types.EffectAllow, "ok", "test-policy"), nil
			})

			expectedLoc := &world.Location{ID: locID, Name: "Loc"}
			expectedExit := &world.Exit{ID: ulid.Make(), Name: "North"}
			objLocID := ulid.Make()
			expectedObj, objErr := world.NewObjectWithID(ulid.Make(), "TestObj", world.Containment{LocationID: &objLocID})
			Expect(objErr).NotTo(HaveOccurred())

			mockLocRepo := worldtest.NewMockLocationRepository(GinkgoT())
			mockLocRepo.On("Get", mock.Anything, mock.Anything).Maybe().Return(expectedLoc, nil)

			mockExitRepo := worldtest.NewMockExitRepository(GinkgoT())
			mockExitRepo.On("Get", mock.Anything, mock.Anything).Maybe().Return(expectedExit, nil)

			mockObjRepo := worldtest.NewMockObjectRepository(GinkgoT())
			mockObjRepo.On("Get", mock.Anything, mock.Anything).Maybe().Return(expectedObj, nil)

			svc := world.NewService(world.ServiceConfig{
				LocationRepo: mockLocRepo,
				ExitRepo:     mockExitRepo,
				ObjectRepo:   mockObjRepo,
				Engine:       engine,
			})

			perType := goroutines / 3
			var wg sync.WaitGroup
			var totalSuccesses, totalFailures atomic.Int32

			// Location goroutines
			for range perType {
				wg.Add(1)
				go func() {
					defer GinkgoRecover()
					defer wg.Done()
					_, err := svc.GetLocation(ctx, subjectID, ulid.Make())
					if err != nil {
						Expect(errors.Is(err, world.ErrAccessEvaluationFailed)).To(BeTrue())
						totalFailures.Add(1)
					} else {
						totalSuccesses.Add(1)
					}
				}()
			}
			// Exit goroutines
			for range perType {
				wg.Add(1)
				go func() {
					defer GinkgoRecover()
					defer wg.Done()
					_, err := svc.GetExit(ctx, subjectID, ulid.Make())
					if err != nil {
						Expect(errors.Is(err, world.ErrAccessEvaluationFailed)).To(BeTrue())
						totalFailures.Add(1)
					} else {
						totalSuccesses.Add(1)
					}
				}()
			}
			// Object goroutines
			for range perType {
				wg.Add(1)
				go func() {
					defer GinkgoRecover()
					defer wg.Done()
					_, err := svc.GetObject(ctx, subjectID, ulid.Make())
					if err != nil {
						Expect(errors.Is(err, world.ErrAccessEvaluationFailed)).To(BeTrue())
						totalFailures.Add(1)
					} else {
						totalSuccesses.Add(1)
					}
				}()
			}
			wg.Wait()

			Expect(totalSuccesses.Load()).To(BeNumerically(">", 0))
			Expect(totalFailures.Load()).To(BeNumerically(">", 0))
			Expect(int(totalSuccesses.Load() + totalFailures.Load())).To(Equal(perType * 3))
		})
	})
})
