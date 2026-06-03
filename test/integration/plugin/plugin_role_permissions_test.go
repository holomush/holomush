// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package plugin_test

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/test/testutil"
)

// Plugin role permissions — INV-CRYPTO-48.
//
// The plugin role provisioned by SchemaProvisioner.ProvisionSchema
// (internal/plugin/schema_provisioner.go) is granted USAGE+CREATE on its
// own schema and has its USAGE on schema public REVOKEd at line 165. As a
// result the plugin role cannot reach host-owned tables (events_audit,
// crypto_keys, etc.) — any attempt MUST fail with PostgreSQL "permission
// denied" before the row is inserted.
//
// This spec pins the regression: it provisions a fresh per-plugin role,
// opens a pool as that role, and asserts an INSERT into events_audit is
// rejected with permission-denied. The substrate has been in place since
// schema_provisioner's introduction; this spec is the explicit cross-
// cutting check that Phase 7 can rely on it.
//
// INV-CRYPTO-48 cross-reference: this spec is the named carrier for the
// invariant; the phase7_boundary_meta_test.go drift detector maps the
// invariant to the suite entry func TestBinaryPlugin (Ginkgo Describes are
// not top-level *testing.T funcs).
var _ = Describe("Plugin role cannot write host tables (INV-CRYPTO-48)", func() {
	It("denies INSERT on host-owned events_audit", func() {
		ctx := context.Background()

		// FreshDatabase runs the migration template, which includes
		// 000009_create_events_audit. The connection string uses the holomush
		// role which has CREATEROLE — required for SchemaProvisioner.Init.
		connStr := testutil.FreshDatabase(suiteT, sharedPG)

		provisioner := plugins.NewSchemaProvisioner(connStr)
		Expect(provisioner.Init(ctx)).NotTo(HaveOccurred())
		defer provisioner.Close()

		pluginConnStr, err := provisioner.ProvisionSchema(ctx, "test-perm-check")
		Expect(err).NotTo(HaveOccurred())

		pluginPool, err := pgxpool.New(ctx, pluginConnStr)
		Expect(err).NotTo(HaveOccurred())
		defer pluginPool.Close()

		// INV-CRYPTO-48: plugin role MUST NOT be able to INSERT into events_audit.
		//
		// Schema-qualify the name (public.events_audit) so Postgres surfaces a
		// permission error rather than the search_path-dependent "relation
		// does not exist" — the plugin role's search_path does not include
		// schema public (USAGE was revoked at schema_provisioner.go:165), so
		// the unqualified name resolves to nothing visible.
		//
		// We need a 16-byte ULID-shaped id (events_audit.id is BYTEA NOT NULL).
		// The Postgres '\x...' bytea literal makes the value explicit.
		_, err = pluginPool.Exec(ctx, `
			INSERT INTO public.events_audit (id, subject, type, timestamp, actor_kind, envelope, schema_ver, codec)
			VALUES ('\x0123456789ABCDEF0123456789ABCDEF', 'events.test.x', 'y', 0, 'system', '\x', 1, 'identity')`)
		Expect(err).To(HaveOccurred(), "INV-CRYPTO-48: plugin role MUST be denied INSERT on host-owned events_audit")

		// The substrate revokes USAGE on schema public from the plugin role
		// (schema_provisioner.go:165). Postgres surfaces this as
		// "permission denied for schema public" before it ever evaluates the
		// table-level INSERT privilege — the schema-USAGE check is the gate
		// that fails first. Either error wording (schema public OR table
		// events_audit) satisfies INV-CRYPTO-48; assert the shared substring.
		Expect(strings.Contains(strings.ToLower(err.Error()), "permission denied")).To(BeTrue(),
			"INV-CRYPTO-48: error MUST be a permission-denied (got: %v)", err)
	})
})
