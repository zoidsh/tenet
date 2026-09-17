// Package provider names the services that answer what a lint asks, and holds
// what tells one from another: the key's environment variable, the host the
// questions go to, and whether it can be used yet.
package provider

import (
	"fmt"
	"strings"

	"github.com/zoidsh/tenet/internal/jev"
)

// Default is the provider a config that names none is judged by.
const Default = "typesafe"

// Provider is one service and the key that buys answers from it.
type Provider struct {
	// Name is what tenets.yml and tenet auth call it, and what a credentials
	// file keys its line by.
	Name string

	// Label is how a sentence names the service rather than the command.
	Label string

	// Env is the environment variable its key is read from.
	Env string

	// BaseURL is empty for a provider the client already defaults to, so that
	// TYPESAFE_BASE_URL keeps pointing a run at a proxy or a local stand-in.
	BaseURL string

	// Summary is the one line that says what a person would be signing up for.
	Summary string

	// Unavailable is why a provider that is named here cannot be used yet,
	// empty for one that works.
	Unavailable string
}

var providers = []Provider{
	{
		Name:    "typesafe",
		Label:   "TypeSafe",
		Env:     jev.APIKeyEnv,
		Summary: "TypeSafe's jev model, from an account at https://typesafe.ai",
	},
	{
		Name:        "tenetlint",
		Label:       "tenetlint",
		Env:         "TENETLINT_API_KEY",
		Summary:     "the hosted tenetlint service",
		Unavailable: "the hosted tenetlint service is not available yet",
	},
}

// All is every provider tenet knows, in the order it lists them.
func All() []Provider {
	out := make([]Provider, len(providers))
	copy(out, providers)
	return out
}

// Names is every provider's name, for a message that has to list them.
func Names() []string {
	names := make([]string, 0, len(providers))
	for _, p := range providers {
		names = append(names, p.Name)
	}
	return names
}

// Lookup is the provider of that name, or what tenet does know instead.
func Lookup(name string) (Provider, error) {
	if name == "" {
		name = Default
	}
	for _, p := range providers {
		if p.Name == name {
			return p, nil
		}
	}
	return Provider{}, fmt.Errorf("unknown provider %q; tenet knows %s", name, strings.Join(Names(), " and "))
}

// Available reports whether there is anything behind the name yet.
func (p Provider) Available() error {
	if p.Unavailable != "" {
		return fmt.Errorf("%s", p.Unavailable)
	}
	return nil
}

// Client is what this provider's key buys answers from. The options come last
// so that a host named on the command line outranks the provider's own.
func (p Provider) Client(key, model string, opts ...jev.Option) *jev.Client {
	own := []jev.Option{jev.WithModel(model)}
	if p.BaseURL != "" {
		own = append(own, jev.WithBaseURL(p.BaseURL))
	}
	return jev.New(key, append(own, opts...)...)
}
