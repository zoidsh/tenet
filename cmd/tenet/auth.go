package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/zoidsh/tenetlint/internal/auth"
	"github.com/zoidsh/tenetlint/internal/jev"
	"github.com/zoidsh/tenetlint/internal/provider"
	"github.com/zoidsh/tenetlint/internal/report"
)

type authOptions struct {
	key      string
	project  bool
	noVerify bool
	status   bool
	remove   bool
}

func newAuthCmd() *cobra.Command {
	o := &authOptions{}
	cmd := &cobra.Command{
		Use:   "auth [provider]",
		Short: "Save the API key a lint is judged with",
		Long: `Save the API key a lint is judged with, so that it is not something to export before every run.

A key is looked for in the provider's environment variable first, then in ` + auth.Dir + `/` + auth.FileName + ` in this repository, then in ~/.config/tenetlint/` + auth.FileName + `. A key saved globally works in every repository, --project keeps one in this repository alone, and CI needs no saved key at all: the environment variable outranks both.

Naming no provider saves the key for the only one there is to save it for, typesafe.

The providers are:

` + providerList(),
		Example: `  tenet auth
  tenet auth typesafe --project
  tenet auth --status
  tenet auth typesafe --remove`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return runAuth(cmd, args, o) },
	}
	f := cmd.Flags()
	f.StringVar(&o.key, "key", "", "the key to save, instead of being asked for it")
	f.BoolVar(&o.project, "project", false, "save the key in this repository instead of globally")
	f.BoolVar(&o.noVerify, "no-verify", false, "save the key without asking the provider whether it works")
	f.BoolVar(&o.status, "status", false, "print which key a run would use and where it was read from, for every provider or the one named")
	f.BoolVar(&o.remove, "remove", false, "delete the saved key instead of saving one")
	return cmd
}

func providerList() string {
	var b strings.Builder
	for _, p := range provider.All() {
		summary := p.Summary
		if p.Unavailable != "" {
			summary += ", not available yet"
		}
		fmt.Fprintf(&b, "  %-10s %s\n", p.Name, summary)
	}
	return b.String()
}

func runAuth(cmd *cobra.Command, args []string, o *authOptions) error {
	if o.status && o.remove {
		return fail(errors.New("--status prints where a key comes from and --remove deletes one, so they cannot be combined"))
	}
	if o.status || o.remove {
		for _, name := range []string{"key", "no-verify"} {
			if cmd.Flags().Changed(name) {
				return fail(fmt.Errorf("--%s is about saving a key, so it cannot be combined with --%s", name, removeOrStatus(o)))
			}
		}
	}
	if o.status {
		return authStatus(cmd, args)
	}
	p, err := namedProvider(args)
	if err != nil {
		return fail(err)
	}
	if err := p.Available(); err != nil {
		return fail(err)
	}
	if o.remove {
		return authRemove(cmd, p, o)
	}
	if _, err := saveKey(cmd, p, o); err != nil {
		return fail(err)
	}
	return nil
}

func removeOrStatus(o *authOptions) string {
	if o.status {
		return "status"
	}
	return "remove"
}

// namedProvider is the provider the arguments name, or the only one a key can
// be saved for: while one provider works there is nothing to choose between,
// and asking would be asking about a list of one.
func namedProvider(args []string) (provider.Provider, error) {
	if len(args) == 1 {
		return provider.Lookup(args[0])
	}
	var available []provider.Provider
	for _, p := range provider.All() {
		if p.Available() == nil {
			available = append(available, p)
		}
	}
	switch len(available) {
	case 1:
		return available[0], nil
	case 0:
		return provider.Provider{}, errors.New("no provider takes a key yet")
	}
	names := make([]string, 0, len(available))
	for _, p := range available {
		names = append(names, p.Name)
	}
	return provider.Provider{}, fmt.Errorf("name the provider to save a key for: %s", strings.Join(names, " or "))
}

// authStatus says where each provider's key comes from and where else it was
// looked for, and never what the key is.
func authStatus(cmd *cobra.Command, args []string) error {
	list := provider.All()
	if len(args) == 1 {
		p, err := provider.Lookup(args[0])
		if err != nil {
			return fail(err)
		}
		list = []provider.Provider{p}
	}
	project, err := auth.ProjectPath()
	if err != nil {
		return fail(err)
	}
	global, err := auth.GlobalPath()
	if err != nil {
		return fail(err)
	}

	out := cmd.OutOrStdout()
	var found bool
	for i, p := range list {
		if i > 0 {
			_, _ = fmt.Fprintln(out)
		}
		if err := p.Available(); err != nil {
			_, _ = fmt.Fprintf(out, "%s: none, %s\n", p.Name, err)
			continue
		}
		key, source, err := auth.Resolve(p)
		if err != nil {
			return fail(err)
		}
		if key == "" {
			_, _ = fmt.Fprintf(out, "%s: none\n", p.Name)
		} else {
			found = true
			_, _ = fmt.Fprintf(out, "%s: the key in %s\n", p.Name, sourceLabel(source))
		}
		_, _ = fmt.Fprintf(out, "  env      %s: %s\n", p.Env, setOrNot(os.Getenv(p.Env)))
		_, _ = fmt.Fprintf(out, "  project  %s\n", fileState(project, p.Name))
		_, _ = fmt.Fprintf(out, "  global   %s\n", fileState(global, p.Name))
	}
	if !found {
		return &exitError{code: report.ExitFinding}
	}
	return nil
}

func sourceLabel(source string) string {
	switch source {
	case auth.SourceEnv:
		return "the environment"
	case auth.SourceProject:
		return "the project file"
	default:
		return "the global file"
	}
}

func setOrNot(value string) string {
	if value == "" {
		return "not set"
	}
	return "set"
}

func fileState(path, name string) string {
	if path == "" {
		return "no repository here to hold one"
	}
	key, err := auth.KeyIn(path, name)
	if err != nil {
		return path + ": " + err.Error()
	}
	if key == "" {
		return path + ": no key"
	}
	return path + ": a key"
}

func authRemove(cmd *cobra.Command, p provider.Provider, o *authOptions) error {
	scope := auth.ScopeGlobal
	if o.project {
		scope = auth.ScopeProject
	}
	path, removed, err := auth.Remove(p, scope)
	if err != nil {
		return fail(err)
	}
	out := cmd.OutOrStdout()
	if !removed {
		_, err := fmt.Fprintf(out, "there was no %s key in %s\n", p.Name, path)
		return err
	}
	if _, err := fmt.Fprintf(out, "removed the %s key from %s\n", p.Name, path); err != nil {
		return err
	}
	key, source, err := auth.Resolve(p)
	if err != nil {
		return fail(err)
	}
	if key == "" {
		return nil
	}
	_, err = fmt.Fprintf(out, "a run still reads a %s key from %s\n", p.Name, sourceLabel(source))
	return err
}

// saveKey is the whole of tenet auth <provider>, and what init runs when
// somebody says yes to being asked for a key.
func saveKey(cmd *cobra.Command, p provider.Provider, o *authOptions) (string, error) {
	key, err := readKey(cmd, p, o)
	if err != nil {
		return "", err
	}
	if err := auth.Validate(key); err != nil {
		return "", err
	}
	if !o.noVerify {
		if err := verifyKey(cmd.Context(), p, key); err != nil {
			return "", err
		}
	}
	saved, err := auth.Save(p, key, !o.project)
	if err != nil {
		return "", err
	}
	out := cmd.OutOrStdout()
	if _, err := fmt.Fprintf(out, "saved the %s key in %s\n", p.Name, saved.Path); err != nil {
		return "", err
	}
	if saved.Gitignore != "" {
		if _, err := fmt.Fprintf(out, "added %s/%s to %s, so the key is not committed\n", auth.Dir, auth.FileName, saved.Gitignore); err != nil {
			return "", err
		}
	}
	if err := sayWhatWins(cmd, p, o); err != nil {
		return "", err
	}
	return key, nil
}

// sayWhatWins is for the key that was saved and will not be used, which is
// otherwise a puzzle: the run after the save reports the same missing key or
// judges with the old one.
func sayWhatWins(cmd *cobra.Command, p provider.Provider, o *authOptions) error {
	scope := auth.SourceGlobal
	if o.project {
		scope = auth.SourceProject
	}
	_, source, err := auth.Resolve(p)
	if err != nil {
		return err
	}
	if source == scope {
		return nil
	}
	out := cmd.OutOrStdout()
	switch source {
	case auth.SourceEnv:
		_, err = fmt.Fprintf(out, "%s is set, so a run reads the key from there instead\n", p.Env)
	case auth.SourceProject:
		path, pathErr := auth.ProjectPath()
		if pathErr != nil {
			return pathErr
		}
		_, err = fmt.Fprintf(out, "%s comes first, so a run in this repository reads the key from there instead\n", path)
	}
	return err
}

// verifyKey asks the smallest question there is, so that a key that will not
// work is caught while somebody is still looking at the screen rather than on
// the next commit.
func verifyKey(ctx context.Context, p provider.Provider, key string) error {
	_, err := p.Client(key, jev.DefaultModel).Ask(ctx, "tenet auth", map[string]jev.Question{
		"verify": jev.Noul("This state is a string.", "", ""),
	})
	var apiErr *jev.APIError
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusUnauthorized {
		return fmt.Errorf("%s rejected that key, so nothing was saved", p.Label)
	}
	if err != nil {
		return fmt.Errorf("could not check the key with %s: %w; pass --no-verify to save it unchecked", p.Label, err)
	}
	return nil
}

func readKey(cmd *cobra.Command, p provider.Provider, o *authOptions) (string, error) {
	// A --key that was typed and came out empty is a shell variable that was
	// not set, which is worth saying rather than quietly asking for a key.
	if cmd.Flags().Changed("key") {
		return strings.TrimSpace(o.key), nil
	}
	if isTerminal(cmd.InOrStdin()) {
		return promptKey(cmd, p)
	}
	return firstLine(cmd.InOrStdin())
}

// exitInterrupted is what a shell expects of a program that Ctrl-C ended:
// 128 plus the signal.
const exitInterrupted = 130

// promptKey turns the echo off, because a key typed at a prompt would
// otherwise stay on the screen, in the scrollback and in a recording of it.
// Ctrl-C is caught for as long as the echo is off: the default handler would
// end the process between turning it off and turning it back on, and leave
// the shell with no echo for everything typed after it.
func promptKey(cmd *cobra.Command, p provider.Provider) (string, error) {
	f, ok := cmd.InOrStdin().(*os.File)
	if !ok {
		return "", errors.New("there is no terminal to ask for a key on")
	}
	fd := int(f.Fd())
	state, err := term.GetState(fd)
	if err != nil {
		return "", err
	}
	interrupted := make(chan os.Signal, 1)
	signal.Notify(interrupted, os.Interrupt)
	defer func() {
		signal.Stop(interrupted)
		close(interrupted)
	}()
	go func() {
		if _, ok := <-interrupted; !ok {
			return
		}
		_ = term.Restore(fd, state)
		_, _ = fmt.Fprintln(cmd.ErrOrStderr())
		os.Exit(exitInterrupted)
	}()

	out := cmd.OutOrStdout()
	if _, err := fmt.Fprintf(out, "%s API key: ", p.Label); err != nil {
		return "", err
	}
	typed, err := term.ReadPassword(fd)
	// The newline the typing ended on was not echoed either.
	_, _ = fmt.Fprintln(out)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(typed)), nil
}

func firstLine(r io.Reader) (string, error) {
	scanner := bufio.NewScanner(r)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", err
		}
		return "", errors.New("read no key on stdin")
	}
	return strings.TrimSpace(scanner.Text()), nil
}

// isTerminal reports whether a stream is a person rather than a pipe, a file
// or a test's buffer.
func isTerminal(stream any) bool {
	f, ok := stream.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}
