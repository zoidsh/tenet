package provider_test

import (
	"strings"
	"testing"

	"github.com/zoidsh/tenet/internal/provider"
)

func TestLookupDefaultsToTypeSafe(t *testing.T) {
	p, err := provider.Lookup("")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != provider.Default || p.Env != "TYPESAFE_API_KEY" {
		t.Errorf("provider is %#v", p)
	}
	if err := p.Available(); err != nil {
		t.Errorf("the default provider is unavailable: %v", err)
	}
}

func TestLookupUnknownNamesTheOnesItKnows(t *testing.T) {
	_, err := provider.Lookup("openai")
	if err == nil {
		t.Fatal("an unknown provider was accepted")
	}
	for _, want := range append(provider.Names(), "openai") {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%q does not name %q", err, want)
		}
	}
}

func TestHostedProviderIsNotAvailableYet(t *testing.T) {
	p, err := provider.Lookup("tenetlint")
	if err != nil {
		t.Fatal(err)
	}
	err = p.Available()
	if err == nil {
		t.Fatal("the hosted service reports itself available")
	}
	if !strings.Contains(err.Error(), "not available yet") {
		t.Errorf("the reason is %q", err)
	}
}

// The default provider names no host of its own, so that TYPESAFE_BASE_URL
// keeps pointing a run at a proxy or a stand-in.
func TestDefaultProviderLeavesTheHostToTheClient(t *testing.T) {
	p, err := provider.Lookup(provider.Default)
	if err != nil {
		t.Fatal(err)
	}
	if p.BaseURL != "" {
		t.Errorf("base URL is %q", p.BaseURL)
	}
	if p.Client("a-key", "jev-1.13.0") == nil {
		t.Error("no client")
	}
}
