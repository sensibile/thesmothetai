// Command thesmo is the Thesmothetai CLI.
//
// Every command except review prints JSON so agents can consume it.
// review is the only way to record a decision and requires an interactive
// terminal.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/sensibile/thesmothetai/internal/collector/hoplites"
	"github.com/sensibile/thesmothetai/internal/interpretation"
	"github.com/sensibile/thesmothetai/internal/review"
	"github.com/sensibile/thesmothetai/internal/store"
	"github.com/sensibile/thesmothetai/schemas"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := rootCmd().ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func defaultDBPath() string {
	if p := os.Getenv("THESMO_DB"); p != "" {
		return p
	}
	return filepath.Join(".thesmothetai", "thesmo.db")
}

func rootCmd() *cobra.Command {
	var dbPath string
	root := &cobra.Command{
		Use:           "thesmo",
		Short:         "License decision registry",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&dbPath, "db", defaultDBPath(), "SQLite database path (env THESMO_DB)")

	open := func(cmd *cobra.Command) (*store.Store, error) {
		if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
			return nil, err
		}
		return store.Open(cmd.Context(), dbPath)
	}

	root.AddCommand(evidenceCmd(open), ingestCmd(open), interpretCmd(open), schemaCmd(), reviewCmd(open))
	return root
}

type opener func(*cobra.Command) (*store.Store, error)

func printJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func evidenceCmd(open opener) *cobra.Command {
	cmd := &cobra.Command{Use: "evidence", Short: "Collect and inspect license evidence"}

	var in store.EvidenceInput
	var textFile string
	add := &cobra.Command{
		Use:   "add",
		Short: "Record license evidence for a component version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			text, err := os.ReadFile(textFile)
			if err != nil {
				return err
			}
			in.Text = text
			st, err := open(cmd)
			if err != nil {
				return err
			}
			defer st.Close()
			res, err := st.AddEvidence(cmd.Context(), in)
			if err != nil {
				return err
			}
			return printJSON(cmd.OutOrStdout(), res)
		},
	}
	add.Flags().StringVar(&in.PURL, "purl", "", "component purl including version")
	add.Flags().StringVar(&in.SPDXExpression, "spdx", "", "declared SPDX expression")
	add.Flags().StringVar(&textFile, "text-file", "", "path to the license text")
	add.Flags().StringVar(&in.Source, "source", "manual", "where the evidence came from")
	for _, f := range []string{"purl", "spdx", "text-file"} {
		add.MarkFlagRequired(f)
	}

	show := &cobra.Command{
		Use:   "show <evidence-id>",
		Short: "Show one evidence record",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := open(cmd)
			if err != nil {
				return err
			}
			defer st.Close()
			ev, err := st.Evidence(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return printJSON(cmd.OutOrStdout(), ev)
		},
	}

	cmd.AddCommand(add, show)
	return cmd
}

func ingestCmd(open opener) *cobra.Command {
	cmd := &cobra.Command{Use: "ingest", Short: "Import license evidence from other systems"}

	fromHoplites := &cobra.Command{
		Use:   "hoplites <run-dir>",
		Short: "Import license-scopes.json and preserved sources from a Hoplites image run",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			run, err := hoplites.Load(args[0])
			if err != nil {
				return err
			}
			files, err := run.Sources()
			if err != nil {
				return err
			}
			st, err := open(cmd)
			if err != nil {
				return err
			}
			defer st.Close()
			res, err := st.Ingest(cmd.Context(), hoplites.IngestInput(run, files))
			if err != nil {
				return err
			}
			return printJSON(cmd.OutOrStdout(), res)
		},
	}

	var outcome string
	records := &cobra.Command{
		Use:   "records <run-id>",
		Short: "List an ingested run's scope records",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := open(cmd)
			if err != nil {
				return err
			}
			defer st.Close()
			recs, err := st.ScopeRecords(cmd.Context(), args[0], outcome)
			if err != nil {
				return err
			}
			return printJSON(cmd.OutOrStdout(), recs)
		},
	}
	records.Flags().StringVar(&outcome, "outcome", "", "filter by outcome: linked, skipped, unresolved")

	cmd.AddCommand(fromHoplites, records)
	return cmd
}

func interpretCmd(open opener) *cobra.Command {
	cmd := &cobra.Command{Use: "interpret", Short: "Agent-facing interpretation workflow"}

	var provider string
	queue := &cobra.Command{
		Use:   "queue",
		Short: "List license texts that need an interpretation",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			st, err := open(cmd)
			if err != nil {
				return err
			}
			defer st.Close()
			items, err := st.Queue(cmd.Context(), provider)
			if err != nil {
				return err
			}
			return printJSON(cmd.OutOrStdout(), items)
		},
	}
	queue.Flags().StringVar(&provider, "provider", "", "skip texts this provider already has an open submission for")

	context := &cobra.Command{
		Use:   "context <source-hash>",
		Short: "Show the text, evidence and previous approval an interpreter needs",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := open(cmd)
			if err != nil {
				return err
			}
			defer st.Close()
			ic, err := st.InterpretContext(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return printJSON(cmd.OutOrStdout(), ic)
		},
	}

	var file string
	submit := &cobra.Command{
		Use:   "submit",
		Short: "Validate and record an interpretation for human review",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var data []byte
			var err error
			if file == "-" {
				data, err = io.ReadAll(cmd.InOrStdin())
			} else {
				data, err = os.ReadFile(file)
			}
			if err != nil {
				return err
			}
			sub, err := interpretation.Parse(data)
			if err != nil {
				return err
			}
			st, err := open(cmd)
			if err != nil {
				return err
			}
			defer st.Close()
			it, err := st.Submit(cmd.Context(), sub)
			if err != nil {
				return err
			}
			return printJSON(cmd.OutOrStdout(), map[string]any{
				"id":          it.ID,
				"status":      it.Status,
				"review_note": it.ReviewNote,
			})
		},
	}
	submit.Flags().StringVarP(&file, "file", "f", "", "submission JSON file, or - for stdin")
	submit.MarkFlagRequired("file")

	var statuses []string
	list := &cobra.Command{
		Use:   "list",
		Short: "List interpretations",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			st, err := open(cmd)
			if err != nil {
				return err
			}
			defer st.Close()
			var ss []interpretation.Status
			for _, s := range statuses {
				ss = append(ss, interpretation.Status(s))
			}
			items, err := st.Interpretations(cmd.Context(), ss...)
			if err != nil {
				return err
			}
			return printJSON(cmd.OutOrStdout(), items)
		},
	}
	list.Flags().StringSliceVar(&statuses, "status", nil, "filter by status (repeatable)")

	show := &cobra.Command{
		Use:   "show <interpretation-id>",
		Short: "Show an interpretation and its decisions",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := open(cmd)
			if err != nil {
				return err
			}
			defer st.Close()
			it, err := st.Interpretation(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			ds, err := st.Decisions(cmd.Context(), it.ID)
			if err != nil {
				return err
			}
			return printJSON(cmd.OutOrStdout(), map[string]any{"interpretation": it, "decisions": ds})
		},
	}

	cmd.AddCommand(queue, context, submit, list, show)
	return cmd
}

func schemaCmd() *cobra.Command {
	return &cobra.Command{
		Use:       "schema interpretation",
		Short:     "Print a JSON schema shared with agents",
		Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		ValidArgs: []string{"interpretation"},
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := cmd.OutOrStdout().Write(schemas.InterpretationV1)
			return err
		},
	}
}

func reviewCmd(open opener) *cobra.Command {
	return &cobra.Command{
		Use:   "review",
		Short: "Review and decide on interpretations (interactive only)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Decisions are a person's call. Agents run commands without a
			// terminal, so refusing non-interactive sessions keeps them out.
			if !isatty.IsTerminal(os.Stdin.Fd()) || !isatty.IsTerminal(os.Stdout.Fd()) {
				return errors.New("review requires an interactive terminal; decisions cannot be scripted")
			}
			u, err := user.Current()
			if err != nil {
				return err
			}
			st, err := open(cmd)
			if err != nil {
				return err
			}
			defer st.Close()
			return review.Run(cmd.Context(), st, u.Username)
		},
	}
}
