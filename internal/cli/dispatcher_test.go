package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/SuperMarioYL/suiter/internal/suite"
)

// fakeSuite is a Suite that records its Read/Write calls, used to prove the
// generic dispatcher routes `suiter <suite> <kind> <verb> [id]` to the
// registered Suite with ZERO per-suite CLI glue.
type fakeSuite struct {
	name    string
	readErr error
	gotKind string
	gotID   string
	readOut []byte
	writeID string
}

func (f *fakeSuite) Name() string { return f.name }
func (f *fakeSuite) Login(context.Context) (suite.Token, error) {
	return suite.Token{}, errors.New("login not used in this test")
}
func (f *fakeSuite) Read(_ context.Context, kind, id string) ([]byte, error) {
	f.gotKind, f.gotID = kind, id
	if f.readErr != nil {
		return nil, f.readErr
	}
	if f.readOut != nil {
		return f.readOut, nil
	}
	return []byte(`{"kind":"` + kind + `","id":"` + id + `"}`), nil
}
func (f *fakeSuite) Write(_ context.Context, kind, id string, body []byte) (string, error) {
	f.gotKind, f.gotID = kind, id
	if f.writeID != "" {
		return f.writeID, nil
	}
	_ = body
	return "new-id", nil
}

// newDispatcherCmd builds a suite dispatcher wired against reg the same way
// NewRoot wires dingtalk/wework, plus a local --json flag (the persistent
// flag normally lives on root).
func newDispatcherCmd(reg *suite.Registry, name string) *cobra.Command {
	cmd := newSuiteCommand(reg, name, "fake suite")
	cmd.PersistentFlags().Bool("json", false, "agent-readable JSON")
	return cmd
}

func runCmd(t *testing.T, cmd *cobra.Command, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		return out.String(), err
	}
	return out.String(), nil
}

// TestDispatcher_ReadVerbs proves read/list/get all route to Suite.Read with
// kind + id positioned correctly.
func TestDispatcher_ReadVerbs(t *testing.T) {
	f := &fakeSuite{name: "fake"}
	reg := suite.NewRegistry()
	reg.Register(f)
	cmd := newDispatcherCmd(reg, "fake")

	for _, verb := range []string{"read", "list", "get"} {
		f.gotKind, f.gotID = "", ""
		out, err := runCmd(t, cmd, "calendar", verb, "cal-1")
		if err != nil {
			t.Fatalf("%s: %v (out=%s)", verb, err, out)
		}
		if f.gotKind != "calendar" || f.gotID != "cal-1" {
			t.Fatalf("%s routed to Read(kind=%q,id=%q), want calendar/cal-1", verb, f.gotKind, f.gotID)
		}
	}
}

// TestDispatcher_WriteVerbs proves create/send/write all route to Suite.Write
// with the stdin body.
func TestDispatcher_WriteVerbs(t *testing.T) {
	f := &fakeSuite{name: "fake", writeID: "evt-1"}
	reg := suite.NewRegistry()
	reg.Register(f)
	cmd := newDispatcherCmd(reg, "fake")
	cmd.SetIn(strings.NewReader(`{"summary":"x"}`))

	for _, verb := range []string{"create", "send", "write"} {
		f.gotKind, f.gotID = "", ""
		out, err := runCmd(t, cmd, "calendar", verb, "primary")
		if err != nil {
			t.Fatalf("%s: %v (out=%s)", verb, err, out)
		}
		if f.gotKind != "calendar" || f.gotID != "primary" {
			t.Fatalf("%s routed to Write(kind=%q,id=%q), want calendar/primary", verb, f.gotKind, f.gotID)
		}
	}
}

// TestDispatcher_JSONRead proves --json indents the Read body.
func TestDispatcher_JSONRead(t *testing.T) {
	f := &fakeSuite{name: "fake", readOut: []byte(`{"a":"b","c":2}`)}
	reg := suite.NewRegistry()
	reg.Register(f)
	cmd := newDispatcherCmd(reg, "fake")
	out, err := runCmd(t, cmd, "calendar", "list", "--json")
	if err != nil {
		t.Fatalf("list --json: %v (out=%s)", err, out)
	}
	if !strings.Contains(out, "  \"a\":") || !strings.Contains(out, "  \"c\":") {
		t.Fatalf("expected pretty JSON, got %q", out)
	}
}

// TestDispatcher_JSONWrite proves --json on a Write emits an {id:...} object.
func TestDispatcher_JSONWrite(t *testing.T) {
	f := &fakeSuite{name: "fake", writeID: "evt-42"}
	reg := suite.NewRegistry()
	reg.Register(f)
	cmd := newDispatcherCmd(reg, "fake")
	cmd.SetIn(strings.NewReader(`{}`))
	out, err := runCmd(t, cmd, "calendar", "create", "--json")
	if err != nil {
		t.Fatalf("create --json: %v (out=%s)", err, out)
	}
	if !strings.Contains(out, `"id"`) || !strings.Contains(out, "evt-42") {
		t.Fatalf("expected {\"id\":\"evt-42\"} JSON, got %q", out)
	}
}

// TestDispatcher_UnregisteredSuite proves a dispatcher whose suite is absent
// errors cleanly.
func TestDispatcher_UnregisteredSuite(t *testing.T) {
	reg := suite.NewRegistry() // nothing registered
	cmd := newDispatcherCmd(reg, "ghost")
	if _, err := runCmd(t, cmd, "calendar", "list"); err == nil {
		t.Fatal("want error for unregistered suite")
	}
}

// TestDispatcher_UnknownVerb proves an unknown verb errors (not a silent stub).
func TestDispatcher_UnknownVerb(t *testing.T) {
	f := &fakeSuite{name: "fake"}
	reg := suite.NewRegistry()
	reg.Register(f)
	cmd := newDispatcherCmd(reg, "fake")
	if _, err := runCmd(t, cmd, "calendar", "frobnicate", "x"); err == nil {
		t.Fatal("want error for unknown verb")
	}
}

// TestDispatcher_MinArgs proves <kind> <verb> are required.
func TestDispatcher_MinArgs(t *testing.T) {
	f := &fakeSuite{name: "fake"}
	reg := suite.NewRegistry()
	reg.Register(f)
	cmd := newDispatcherCmd(reg, "fake")
	if _, err := runCmd(t, cmd, "calendar"); err == nil {
		t.Fatal("want error when only one arg given (need kind+verb)")
	}
}
