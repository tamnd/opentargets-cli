package opentargets

import (
	"strings"
	"testing"

	"github.com/tamnd/any-cli/kit"
)

// TestDomainInfo verifies the scheme, hosts, and binary name.
func TestDomainInfo(t *testing.T) {
	info := Domain{}.Info()
	if info.Scheme != "opentargets" {
		t.Errorf("Scheme = %q, want opentargets", info.Scheme)
	}
	if len(info.Hosts) == 0 || info.Hosts[0] != Host {
		t.Errorf("Hosts = %v, want [%s]", info.Hosts, Host)
	}
	if info.Identity.Binary != "opentargets" {
		t.Errorf("Identity.Binary = %q, want opentargets", info.Identity.Binary)
	}
}

// TestClassify checks that any non-empty input is accepted as a target.
func TestClassify(t *testing.T) {
	cases := []struct {
		in  string
		typ string
		id  string
	}{
		{"ENSG00000141510", "target", "ENSG00000141510"},
		{"EFO_0000311", "target", "EFO_0000311"},
		{"TP53", "target", "TP53"},
	}
	for _, tc := range cases {
		typ, id, err := Domain{}.Classify(tc.in)
		if err != nil {
			t.Errorf("Classify(%q) error: %v", tc.in, err)
			continue
		}
		if typ != tc.typ || id != tc.id {
			t.Errorf("Classify(%q) = (%q, %q), want (%q, %q)",
				tc.in, typ, id, tc.typ, tc.id)
		}
	}
}

// TestClassifyEmpty checks that an empty input returns an error.
func TestClassifyEmpty(t *testing.T) {
	_, _, err := Domain{}.Classify("")
	if err == nil {
		t.Error("Classify(\"\") should return an error")
	}
}

// TestLocate checks the URL for target and disease resource types.
func TestLocate(t *testing.T) {
	tests := []struct {
		uriType string
		id      string
		want    string
	}{
		{"target", "ENSG00000141510", platformURL + "/target/ENSG00000141510"},
		{"disease", "EFO_0000311", platformURL + "/disease/EFO_0000311"},
	}
	for _, tc := range tests {
		got, err := Domain{}.Locate(tc.uriType, tc.id)
		if err != nil {
			t.Errorf("Locate(%q, %q) error: %v", tc.uriType, tc.id, err)
			continue
		}
		if got != tc.want {
			t.Errorf("Locate(%q, %q) = %q, want %q", tc.uriType, tc.id, got, tc.want)
		}
	}
}

// TestLocateUnknownType checks that an unknown URI type returns an error.
func TestLocateUnknownType(t *testing.T) {
	_, err := Domain{}.Locate("page", "foo")
	if err == nil {
		t.Error("Locate(\"page\", ...) should return an error")
	}
}

// TestLocateContainsID verifies the ID appears in the returned URL.
func TestLocateContainsID(t *testing.T) {
	id := "ENSG00000141510"
	got, err := Domain{}.Locate("target", id)
	if err != nil {
		t.Fatalf("Locate: %v", err)
	}
	if !strings.Contains(got, id) {
		t.Errorf("Locate(%q) = %q, does not contain id", id, got)
	}
}

// TestDomainRegister checks that the Domain registers without panicking and
// the kit host finds the opentargets scheme.
func TestDomainRegister(t *testing.T) {
	h, err := kit.Open()
	if err != nil {
		t.Fatal(err)
	}

	// Mint a Target record and verify its URI scheme.
	tgt := &Target{ID: "ENSG00000141510", Symbol: "TP53"}
	u, err := h.Mint(tgt)
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if u.Scheme != "opentargets" {
		t.Errorf("URI scheme = %q, want opentargets", u.Scheme)
	}
}
