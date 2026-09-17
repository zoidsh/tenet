package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/zoidsh/tenet/internal/tenets"
)

func newConfigCmd() *cobra.Command {
	var path string
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Print the tenets your config resolves to and where each came from",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := openConfig(path)
			if err != nil {
				return fail(err)
			}
			var b strings.Builder
			b.WriteString(cfg.Path + "\n\n")
			table := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprint(table, "id\torigin\tkind\tfail\tinclude\n")
			for _, t := range cfg.Tenets {
				_, _ = fmt.Fprintf(table, "%s\t%s\t%s\t%.2f\t%s\n",
					t.ID, t.Origin, list(t.Kind), t.FailValue(), list(t.Include))
			}
			if err := table.Flush(); err != nil {
				return fail(err)
			}
			_, err = io.WriteString(cmd.OutOrStdout(), b.String())
			return err
		},
	}
	cmd.Flags().StringVar(&path, "config", "", "path to tenet.yml, searched for by default")
	return cmd
}

// openConfig reads the config a command was pointed at, or the one the
// directory it was run from belongs to.
func openConfig(path string) (*tenets.Config, error) {
	if path == "" {
		dir, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		found, _, err := tenets.Find(dir)
		if err != nil {
			return nil, err
		}
		path = found
	}
	return tenets.Load(path)
}
