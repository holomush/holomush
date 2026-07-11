// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/samber/oops"
	"github.com/spf13/cobra"

	"github.com/holomush/holomush/internal/config"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/audit"
)

// auditDLQScanTimeout bounds a single list/show scan of the DLQ stream.
const auditDLQScanTimeout = 30 * time.Second

// NewAuditCmd returns the `holomush audit` parent command. Its subcommands
// operate the durable audit trail from the operator host; unlike the crypto
// commands they need NO admin UDS — the DLQ tools read the EVENTS_AUDIT_DLQ
// JetStream stream and write the events_audit Postgres table directly
// (CLUSTER-04, OQ-5).
func NewAuditCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Audit-trail operator commands (NATS + Postgres, no admin UDS)",
	}
	cmd.AddCommand(newAuditDLQCmd())
	return cmd
}

// newAuditDLQCmd returns the `holomush audit dlq` subgroup: list / show /
// replay over the audit dead-letter stream.
func newAuditDLQCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dlq",
		Short: "Inspect and replay the audit dead-letter queue (EVENTS_AUDIT_DLQ)",
		Long: `Inspect and replay the audit dead-letter queue.

Audit messages that exhaust redelivery are captured to the bounded
EVENTS_AUDIT_DLQ stream instead of being dropped. Once the underlying
outage (usually Postgres) is fixed, 'replay' re-drives those dead letters
back into events_audit through the same idempotent write path the live
projection uses — dead letters are recoverable, not data loss.`,
	}
	cmd.AddCommand(newAuditDLQListCmd())
	cmd.AddCommand(newAuditDLQShowCmd())
	cmd.AddCommand(newAuditDLQReplayCmd())
	return cmd
}

// newAuditDLQListCmd returns `holomush audit dlq list`: a stream-level
// summary (message count, byte size, oldest/newest timestamps).
func newAuditDLQListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Summarize the audit dead-letter stream",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadEventBusConfig(cmd)
			if err != nil {
				return err
			}
			conn, js, err := dialAuditJetStream(cfg)
			if err != nil {
				return err
			}
			defer conn.Close()
			return runAuditDLQList(cmd.Context(), js, cmd.OutOrStdout())
		},
	}
}

// newAuditDLQShowCmd returns `holomush audit dlq show <nats-msg-id>`: prints
// a single dead letter's subject, headers, and stream metadata.
func newAuditDLQShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <nats-msg-id>",
		Short: "Show a single dead letter's headers and metadata by Nats-Msg-Id",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadEventBusConfig(cmd)
			if err != nil {
				return err
			}
			conn, js, err := dialAuditJetStream(cfg)
			if err != nil {
				return err
			}
			defer conn.Close()
			return runAuditDLQShow(cmd.Context(), js, args[0], cmd.OutOrStdout())
		},
	}
}

// newAuditDLQReplayCmd returns `holomush audit dlq replay`: re-drives dead
// letters back into events_audit. --all replays everything, --msg-id
// targets a single entry, --limit caps a batch.
func newAuditDLQReplayCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "replay",
		Short: "Replay dead letters back into events_audit (idempotent)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAuditDLQReplay(cmd)
		},
	}
	cmd.Flags().Bool("all", false, "Replay every dead letter in the stream")
	cmd.Flags().String("msg-id", "", "Replay only the dead letter with this Nats-Msg-Id")
	cmd.Flags().Int("limit", 0, "Cap the number of dead letters scanned in one pass (0 = no cap)")
	return cmd
}

// dialAuditJetStream dials the external NATS cluster described by cfg via
// the shared eventbus dial path and returns a JetStream handle. The caller
// owns the returned *nats.Conn and MUST Close it.
func dialAuditJetStream(cfg eventbus.Config) (*nats.Conn, jetstream.JetStream, error) {
	if cfg.URL == "" {
		return nil, nil, oops.Code("AUDIT_DLQ_NATS_URL_MISSING").
			Errorf("event_bus.url is required (external NATS URL) for audit dlq commands")
	}
	conn, err := eventbus.Dial(cfg)
	if err != nil {
		return nil, nil, oops.Code("AUDIT_DLQ_NATS_DIAL_FAILED").Wrap(err)
	}
	js, err := jetstream.New(conn)
	if err != nil {
		conn.Close()
		return nil, nil, oops.Code("AUDIT_DLQ_JETSTREAM_FAILED").Wrap(err)
	}
	return conn, js, nil
}

// loadEventBusConfig reads the event_bus config section (URL / creds / TLS)
// the DLQ commands dial with.
func loadEventBusConfig(cmd *cobra.Command) (eventbus.Config, error) {
	cfg := eventbus.Config{}
	if err := config.Load(configFile, cmd, &cfg, "event_bus"); err != nil {
		return cfg, oops.Code("AUDIT_DLQ_CONFIG_FAILED").Wrap(err)
	}
	return cfg, nil
}

// openAuditPool opens a pgxpool against DATABASE_URL — the durable
// events_audit table replay writes to.
func openAuditPool(ctx context.Context) (*pgxpool.Pool, error) {
	url, err := getDatabaseURL()
	if err != nil {
		return nil, oops.Code("AUDIT_DLQ_DATABASE_URL_MISSING").Wrap(err)
	}
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, oops.Code("AUDIT_DLQ_POOL_FAILED").Wrap(err)
	}
	return pool, nil
}

// runAuditDLQList prints a summary of the EVENTS_AUDIT_DLQ stream.
func runAuditDLQList(ctx context.Context, js jetstream.JetStream, w io.Writer) error {
	ctx, cancel := context.WithTimeout(ctx, auditDLQScanTimeout)
	defer cancel()

	stream, err := js.Stream(ctx, audit.DefaultDLQStreamName)
	if err != nil {
		return oops.Code("AUDIT_DLQ_STREAM_LOOKUP_FAILED").
			With("stream", audit.DefaultDLQStreamName).
			Wrap(err)
	}
	info, err := stream.Info(ctx)
	if err != nil {
		return oops.Code("AUDIT_DLQ_STREAM_INFO_FAILED").
			With("stream", audit.DefaultDLQStreamName).
			Wrap(err)
	}
	renderDLQInfo(w, info.State)
	return nil
}

// renderDLQInfo prints a StreamState summary in human-readable form.
func renderDLQInfo(w io.Writer, state jetstream.StreamState) {
	fmt.Fprintf(w, "stream:   %s\n", audit.DefaultDLQStreamName) //nolint:errcheck // display output; write errors non-fatal
	fmt.Fprintf(w, "messages: %d\n", state.Msgs)                 //nolint:errcheck // display output; write errors non-fatal
	fmt.Fprintf(w, "bytes:    %d\n", state.Bytes)                //nolint:errcheck // display output; write errors non-fatal
	if state.Msgs == 0 {
		fmt.Fprintln(w, "(dead-letter queue is empty)") //nolint:errcheck // display output; write errors non-fatal
		return
	}
	fmt.Fprintf(w, "oldest:   %s (seq %d)\n", //nolint:errcheck // display output; write errors non-fatal
		state.FirstTime.UTC().Format(time.RFC3339), state.FirstSeq)
	fmt.Fprintf(w, "newest:   %s (seq %d)\n", //nolint:errcheck // display output; write errors non-fatal
		state.LastTime.UTC().Format(time.RFC3339), state.LastSeq)
}

// runAuditDLQShow scans the DLQ stream for the message whose Nats-Msg-Id
// header equals msgID and prints its subject, headers, and metadata.
func runAuditDLQShow(ctx context.Context, js jetstream.JetStream, msgID string, w io.Writer) error {
	ctx, cancel := context.WithTimeout(ctx, auditDLQScanTimeout)
	defer cancel()

	stream, err := js.Stream(ctx, audit.DefaultDLQStreamName)
	if err != nil {
		return oops.Code("AUDIT_DLQ_STREAM_LOOKUP_FAILED").
			With("stream", audit.DefaultDLQStreamName).
			Wrap(err)
	}
	info, err := stream.Info(ctx)
	if err != nil {
		return oops.Code("AUDIT_DLQ_STREAM_INFO_FAILED").
			With("stream", audit.DefaultDLQStreamName).
			Wrap(err)
	}
	budget := int(info.State.Msgs) //nolint:gosec // stream msg count is bounded well within int
	if budget == 0 {
		return oops.Code("AUDIT_DLQ_MESSAGE_NOT_FOUND").
			With("msg_id", msgID).
			Errorf("dead-letter queue is empty")
	}

	cons, err := stream.OrderedConsumer(ctx, jetstream.OrderedConsumerConfig{
		DeliverPolicy: jetstream.DeliverAllPolicy,
	})
	if err != nil {
		return oops.Code("AUDIT_DLQ_CONSUMER_FAILED").Wrap(err)
	}

	scanned := 0
	for scanned < budget {
		if ctx.Err() != nil {
			return oops.Code("AUDIT_DLQ_SHOW_CANCELLED").Wrap(ctx.Err())
		}
		batch, ferr := cons.Fetch(budget-scanned, jetstream.FetchMaxWait(500*time.Millisecond))
		if ferr != nil && !isFetchTimeout(ferr) {
			return oops.Code("AUDIT_DLQ_FETCH_FAILED").Wrap(ferr)
		}
		// Defensive: a timeout error is expected to come with a non-nil batch
		// (jetstream's current behavior), but guard against a nil batch so a
		// future client change cannot turn this into a nil-deref panic.
		if batch == nil {
			break
		}
		got := 0
		for msg := range batch.Messages() {
			got++
			scanned++
			if msg.Headers().Get(audit.HeaderMsgID) == msgID {
				renderDLQMessage(w, msg)
				return nil
			}
			_ = msg.Ack() //nolint:errcheck // ack advances the cursor; LimitsPolicy retains the message
		}
		// A delivery/fetch error surfaces via batch.Error() after the range,
		// not from Fetch. Without this check an error is swallowed and the
		// command falls through to a misleading AUDIT_DLQ_MESSAGE_NOT_FOUND.
		if err := batch.Error(); err != nil && !isFetchTimeout(err) {
			return oops.Code("AUDIT_DLQ_FETCH_FAILED").Wrap(err)
		}
		if got == 0 {
			break
		}
	}
	return oops.Code("AUDIT_DLQ_MESSAGE_NOT_FOUND").
		With("msg_id", msgID).
		Errorf("no dead letter with Nats-Msg-Id %q", msgID)
}

// renderDLQMessage prints one dead letter's subject, metadata, and headers.
func renderDLQMessage(w io.Writer, msg jetstream.Msg) {
	fmt.Fprintf(w, "dlq-subject: %s\n", msg.Subject()) //nolint:errcheck // display output; write errors non-fatal
	if meta, err := msg.Metadata(); err == nil {
		fmt.Fprintf(w, "dlq-seq:     %d\n", meta.Sequence.Stream)                      //nolint:errcheck // display output; write errors non-fatal
		fmt.Fprintf(w, "captured-at: %s\n", meta.Timestamp.UTC().Format(time.RFC3339)) //nolint:errcheck // display output; write errors non-fatal
	}
	fmt.Fprintln(w, "headers:") //nolint:errcheck // display output; write errors non-fatal
	for k, vals := range msg.Headers() {
		for _, v := range vals {
			fmt.Fprintf(w, "  %s: %s\n", k, v) //nolint:errcheck // display output; write errors non-fatal
		}
	}
	fmt.Fprintf(w, "data-bytes:  %d\n", len(msg.Data())) //nolint:errcheck // display output; write errors non-fatal
}

// isFetchTimeout reports whether err is the benign no-messages-in-window
// timeout returned by a JetStream Fetch.
func isFetchTimeout(err error) bool {
	return errors.Is(err, nats.ErrTimeout)
}

// runAuditDLQReplay wires audit.ReplayDLQ with a NATS + Postgres handle and
// renders the replay result. It validates the --all / --msg-id / --limit
// flag combination before touching the network.
func runAuditDLQReplay(cmd *cobra.Command) error {
	all, _ := cmd.Flags().GetBool("all")        //nolint:errcheck // flag defined in newAuditDLQReplayCmd; absence is a programmer error
	msgID, _ := cmd.Flags().GetString("msg-id") //nolint:errcheck // flag defined in newAuditDLQReplayCmd; absence is a programmer error
	limit, _ := cmd.Flags().GetInt("limit")     //nolint:errcheck // flag defined in newAuditDLQReplayCmd; absence is a programmer error

	opts, err := replayOptsFromFlags(all, msgID, limit)
	if err != nil {
		return err
	}

	cfg, err := loadEventBusConfig(cmd)
	if err != nil {
		return err
	}

	conn, js, err := dialAuditJetStream(cfg)
	if err != nil {
		return err
	}
	defer conn.Close()

	pool, err := openAuditPool(cmd.Context())
	if err != nil {
		return err
	}
	defer pool.Close()

	res, err := audit.ReplayDLQ(cmd.Context(), js, pool, dlqConfigForGame(cfg.GameID), opts)
	if err != nil {
		return oops.Code("AUDIT_DLQ_REPLAY_FAILED").Wrap(err)
	}
	renderReplayResult(cmd.OutOrStdout(), res)
	return nil
}

// dlqConfigForGame builds the DLQConfig ReplayDLQ needs. The stream name is
// the fixed EVENTS_AUDIT_DLQ (via Defaults); the subject is game-scoped
// (internal.<game_id>.audit.dlq, mirroring core.go) so replay can recover
// each dead letter's original event subject from its DLQ subject suffix.
func dlqConfigForGame(gameID string) audit.DLQConfig {
	cfg := audit.DLQConfig{}
	if gameID != "" {
		cfg.Subject = fmt.Sprintf("internal.%s.audit.dlq", gameID)
	}
	return cfg
}

// replayOptsFromFlags validates the replay flag combination and maps it to
// audit.ReplayOptions. Exactly one selection mode is required: --all,
// --msg-id, or a positive --limit.
func replayOptsFromFlags(all bool, msgID string, limit int) (audit.ReplayOptions, error) {
	if all && msgID != "" {
		return audit.ReplayOptions{}, oops.Code("EX_USAGE").
			Errorf("--all and --msg-id are mutually exclusive")
	}
	if !all && msgID == "" && limit <= 0 {
		return audit.ReplayOptions{}, oops.Code("EX_USAGE").
			Errorf("specify --all, --msg-id <id>, or --limit <n>")
	}
	return audit.ReplayOptions{MsgID: msgID, Limit: limit}, nil
}

// renderReplayResult prints a ReplayResult summary.
func renderReplayResult(w io.Writer, res audit.ReplayResult) {
	fmt.Fprintf(w, "scanned:  %d\n", res.Scanned)  //nolint:errcheck // display output; write errors non-fatal
	fmt.Fprintf(w, "replayed: %d\n", res.Replayed) //nolint:errcheck // display output; write errors non-fatal
	if res.Skipped > 0 {
		fmt.Fprintf(w, "skipped:  %d\n", res.Skipped) //nolint:errcheck // display output; write errors non-fatal
	}
	fmt.Fprintf(w, "failed:   %d\n", res.Failed) //nolint:errcheck // display output; write errors non-fatal
	if res.Failed > 0 {
		fmt.Fprintln(w, "(failed dead letters are retained in the DLQ for inspection)") //nolint:errcheck // display output; write errors non-fatal
	}
}
