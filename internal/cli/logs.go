package cli

import (
	"fmt"
	"time"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"github.com/trustgate/trustgate/internal/audit"
	"github.com/trustgate/trustgate/internal/config"
)

func newLogsCmd() *cobra.Command {
	var action string
	var user string
	var session string
	var since string
	var limit int
	var formatJSON bool
	var verify bool

	cmd := &cobra.Command{
		Use:   "logs",
		Short: "View audit logs",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cfgFile)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			if cfg.Mode == "standalone" || cfg.Mode == "" {
				cmd.Println("Standalone mode uses in-memory logs only (available while Agent is running).")
				cmd.Println("Use: curl http://localhost:8787/v1/audit/logs")
				return nil
			}

			auditDir := cfg.Audit.Path
			if auditDir == "" {
				auditDir = "audit_data"
			}

			logger := zerolog.Nop()
			wal, err := audit.NewWALWriter(auditDir, logger)
			if err != nil {
				return fmt.Errorf("open audit WAL: %w", err)
			}
			defer wal.Close()

			// Verify chain integrity if requested
			if verify {
				valid, verr := wal.VerifyChain()
				if verr != nil {
					cmd.Printf("Chain integrity: FAILED at record %d — %v\n", valid, verr)
					return nil
				}
				cmd.Printf("Chain integrity: OK (%d records verified)\n", valid)
				cursor := wal.CursorState()
				cmd.Printf("Cursor: flushed_seq=%d, flushed_at=%s\n", cursor.FlushedSeq, cursor.FlushedAt)
				return nil
			}

			opts := audit.QueryOpts{
				Action:    action,
				UserID:    user,
				SessionID: session,
				Limit:     limit,
			}

			if since != "" {
				d, err := time.ParseDuration(since)
				if err != nil {
					return fmt.Errorf("invalid --since value: %w", err)
				}
				opts.Since = time.Now().Add(-d)
			}

			records, err := wal.Query(opts)
			if err != nil {
				return fmt.Errorf("query audit logs: %w", err)
			}

			if len(records) == 0 {
				cmd.Println("No audit logs found.")
				return nil
			}

			if formatJSON {
				for _, r := range records {
					cmd.Printf(`{"audit_id":"%s","timestamp":"%s","user":"%s","action":"%s","policy":"%s","reason":"%s","site":"%s","duration_ms":%d}`+"\n",
						r.AuditID, r.Timestamp.Format(time.RFC3339), r.UserID, r.Action, r.PolicyName, r.Reason, r.AppID, r.DurationMs)
				}
			} else {
				cmd.Printf("%-20s %-7s %-12s %-28s %s\n", "TIMESTAMP", "ACTION", "USER", "POLICY", "REASON")
				cmd.Println("─────────────────── ─────── ──────────── ──────────────────────────── ──────────────────────")
				for _, r := range records {
					ts := r.Timestamp.Format("2006-01-02 15:04:05")
					cmd.Printf("%-20s %-7s %-12s %-28s %s\n", ts, r.Action, r.UserID, r.PolicyName, r.Reason)
				}
				cmd.Printf("\n%d records\n", len(records))
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&action, "action", "", "filter by action (ALLOW|WARN|MASK|BLOCK)")
	cmd.Flags().StringVar(&user, "user", "", "filter by user ID")
	cmd.Flags().StringVar(&session, "session", "", "filter by session ID")
	cmd.Flags().StringVar(&since, "since", "", "show logs since duration (e.g. 1h, 24h, 7d)")
	cmd.Flags().IntVar(&limit, "limit", 50, "max number of records")
	cmd.Flags().BoolVar(&formatJSON, "json", false, "output in JSON format")
	cmd.Flags().BoolVar(&verify, "verify", false, "verify hash chain integrity")

	return cmd
}
