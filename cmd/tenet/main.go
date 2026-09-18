// Command tenet lints code against English rules.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/zoidsh/tenet/internal/buildinfo"
	"github.com/zoidsh/tenet/internal/jev"
	"github.com/zoidsh/tenet/internal/provider"
	"github.com/zoidsh/tenet/internal/report"
)

// keyFlagSuffix ends the name of every flag that carries an API key, which is
// how anything writing a command line down knows what not to write.
const keyFlagSuffix = "-api-key"

// providerFlags are the pair of flags one provider answers to, named after it
// so that a second provider brings its own pair rather than fighting over one.
type providerFlags struct {
	key     string
	baseURL string
}

// globalOptions are what every command reads off the root's persistent flags.
type globalOptions struct {
	providers map[string]*providerFlags
	color     string
}

// addGlobalFlags gives each provider that takes a key today its own pair. A
// provider that is not available yet has nothing to point at, so it has no
// flags either.
func addGlobalFlags(root *cobra.Command) *globalOptions {
	g := &globalOptions{providers: map[string]*providerFlags{}}
	f := root.PersistentFlags()
	for _, p := range provider.All() {
		if p.Available() != nil {
			continue
		}
		pf := &providerFlags{}
		g.providers[p.Name] = pf
		f.StringVar(&pf.key, p.Name+keyFlagSuffix, "",
			"the "+p.Label+" API key to judge with, ahead of "+p.Env+" and any saved key; a saved key or the variable is better, because a flag is in the process list for anyone on the machine to read")
		f.StringVar(&pf.baseURL, p.Name+"-base-url", "",
			"send the "+p.Label+" requests to this host, ahead of the environment and the host "+p.Name+" is otherwise asked on")
	}
	f.StringVar(&g.color, "color", report.ColorAuto, "colour in the text report: auto, always or never, ahead of NO_COLOR")
	return g
}

// key is the key the flags carry for a provider, empty when they carry none.
func (g *globalOptions) key(p provider.Provider) string {
	if pf, ok := g.providers[p.Name]; ok {
		return pf.key
	}
	return ""
}

// clientOptions are what the flags add to a provider's own client, which is a
// host to ask instead of the usual one.
func (g *globalOptions) clientOptions(p provider.Provider) []jev.Option {
	if pf, ok := g.providers[p.Name]; ok && pf.baseURL != "" {
		return []jev.Option{jev.WithBaseURL(pf.baseURL)}
	}
	return nil
}

func newRootCmd() *cobra.Command {
	opts := &lintOptions{}
	root := &cobra.Command{
		Use:   "tenet [paths...]",
		Short: "The review gate for code that agents write",
		// Without this, cobra reads the first path as the name of a subcommand
		// it does not have.
		Args:          cobra.ArbitraryArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLint(cmd, args, opts)
		},
	}
	// The flag and the subcommand print the same one line, because a script
	// reading either should not have to know which one it asked.
	root.Version = buildinfo.Version()
	root.SetVersionTemplate("{{.Version}}\n")
	g := addGlobalFlags(root)
	opts.g = g
	addLintFlags(root, opts)
	root.AddCommand(newVersionCmd(), newAuthCmd(g), newInitCmd(g), newSyncCmd(g), newHookCmd(), newCheckCmd(g), newRulesCmd(), newPresetsCmd(), newConfigCmd(), newBaselineCmd(g))
	return root
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the tenet version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), buildinfo.Version())
			return err
		},
	}
}

func execute(root *cobra.Command) int {
	err := root.Execute()
	if err == nil {
		return report.ExitOK
	}
	var exit *exitError
	if errors.As(err, &exit) {
		if exit.err != nil {
			_, _ = fmt.Fprintln(root.ErrOrStderr(), "tenet:", exit.err)
		}
		return exit.code
	}
	_, _ = fmt.Fprintln(root.ErrOrStderr(), "tenet:", err)
	return report.ExitError
}

func main() {
	os.Exit(execute(newRootCmd()))
}
