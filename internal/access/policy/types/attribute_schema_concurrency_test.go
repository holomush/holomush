// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package types

import (
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAttributeSchemaSurvivesConcurrentRegistrationAndLookup pins the guarantee
// the AttributeSchema doc comment makes: every method is safe for concurrent
// use, so no holder of a shared schema has to rely on external ordering.
//
// The guarantee, not the lock, is the thing under test — a future edit that
// replaced the RWMutex with a sync.Map or a copy-on-write pointer swap should
// keep this green. What it must NOT be allowed to do is silently drop the
// property, which is the failure this test exists to prevent.
//
// WHY IT MATTERS HERE. Since phase 02.2-04 the production policy compiler is
// built on the LIVE schema that attribute.Resolver's provider registration
// writes into, so a schema read (a policy cache reload, on the poller's own
// goroutine) and a schema write (a plugin load or unload) address one map. They
// do not overlap today, but only because of boot-phase ordering nothing pins:
// all writes happen in PluginSubsystem.Prepare, plugin loading is sequential,
// UnloadPlugin has no production caller, and the lifecycle orchestrator's global
// barrier keeps the poller from starting until every Prepare has returned. A
// hot-reload, a first UnloadPlugin caller, or concurrency inside LoadAll each
// removes that accident.
//
// The failure mode this guards is NOT a race-detector warning that CI would flag
// politely: Go aborts an unsynchronised concurrent map read/write with
// `fatal error: concurrent map read and map write`, which kills the process. The
// unit suite runs under -race, so an unguarded accessor reintroduced later fails
// here (or aborts the run outright) rather than in production.
func TestAttributeSchemaSurvivesConcurrentRegistrationAndLookup(t *testing.T) {
	t.Parallel()

	const (
		writers = 4
		readers = 4
		rounds  = 200
	)

	s := NewAttributeSchema()

	// A namespace that is present for the whole run, so the readers below assert
	// a real answer rather than only exercising the miss path.
	require.NoError(t, s.Register("stable", &NamespaceSchema{
		Attributes: map[string]AttrType{"id": AttrTypeString},
	}))

	var wg sync.WaitGroup

	for w := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ns := churnNamespaceName(w)
			for range rounds {
				_ = s.Register(ns, &NamespaceSchema{
					Attributes: map[string]AttrType{"name": AttrTypeString},
				})
				s.Replace(ns, &NamespaceSchema{
					Attributes: map[string]AttrType{"name": AttrTypeString, "extra": AttrTypeBool},
				})
				s.Remove(ns)
			}
		}()
	}

	for range readers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range rounds {
				// The stable namespace must read back correctly on every
				// iteration; a torn or missing answer here is a real defect, not
				// merely a synchronisation smell.
				if !s.HasNamespace("stable") || !s.IsRegistered("stable", "id") {
					// assert from the goroutine rather than require: require's
					// FailNow from a non-test goroutine is undefined behaviour.
					assert.Fail(t, "the stable namespace must be visible on every read")
					return
				}
				if ns := s.GetNamespace("stable"); ns == nil {
					assert.Fail(t, "GetNamespace must return the registered stable namespace")
					return
				}
				// Churned namespaces: the ANSWER is racy by construction (a
				// writer may have removed it), so nothing is asserted about it.
				// Reading it is the point — these are the calls that would abort
				// the process against an unguarded map.
				_ = s.HasNamespace(churnNamespaceName(0))
				_ = s.IsRegistered(churnNamespaceName(1), "name")
				_ = s.GetNamespace(churnNamespaceName(2))
			}
		}()
	}

	wg.Wait()

	assert.True(t, s.HasNamespace("stable"),
		"control: the schema must still be usable after the churn")
	assert.True(t, s.IsRegistered("stable", "id"))
}

// churnNamespaceName builds a distinct namespace per writer goroutine, so the
// writers contend on the map itself rather than on a single key.
func churnNamespaceName(i int) string {
	return "churn-" + strconv.Itoa(i)
}
