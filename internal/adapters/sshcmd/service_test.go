package sshcmd

import (
	"context"
	"errors"
	"testing"

	"github.com/MiguelFPP/tui-ssh/internal/domain"
)

func TestSessionCommand(t *testing.T) {
	session := New("ssh")
	session.lookPath = func(name string) (string, error) {
		if name != "ssh" {
			t.Errorf("lookpath(%q), want ssh", name)
		}

		return "/usr/bin/ssh", nil
	}

	command, err := session.Command(context.Background(), "gd-vps")
	if err != nil {
		t.Fatalf("Command: %v", err)
	}

	if command.Binary != "/usr/bin/ssh" {
		t.Errorf("Binary = %q", command.Binary)
	}
	if len(command.Args) != 1 || command.Args[0] != "gd-vps" {
		t.Errorf("Args = %v, want the alias alone", command.Args)
	}
}

func TestSessionCommandDefaultsBinary(t *testing.T) {
	session := New("")
	session.lookPath = func(name string) (string, error) {
		if name != "ssh" {
			t.Errorf("lookpath(%q), want the ssh default", name)
		}

		return "/usr/bin/ssh", nil
	}

	if _, err := session.Command(context.Background(), "web"); err != nil {
		t.Fatalf("Command: %v", err)
	}
}

func TestSessionCommandUnavailable(t *testing.T) {
	session := New("ssh")
	session.lookPath = func(string) (string, error) {
		return "", errors.New("not found")
	}

	_, err := session.Command(context.Background(), "web")
	if !errors.Is(err, domain.ErrSSHUnavailable) {
		t.Fatalf("Command = %v, want ErrSSHUnavailable", err)
	}
}

func TestSessionCommandRejectsInvalidAlias(t *testing.T) {
	session := New("ssh")
	session.lookPath = func(string) (string, error) {
		t.Fatal("lookpath must not run for an invalid alias")

		return "", nil
	}

	if _, err := session.Command(context.Background(), "two words"); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("Command = %v, want ErrInvalidArgument", err)
	}
}

func TestConfigEditorResolve(t *testing.T) {
	editor, err := NewConfigEditor("vim -u NONE")
	if err != nil {
		t.Fatalf("NewConfigEditor: %v", err)
	}
	editor.lookPath = func(string) (string, error) { return "/usr/bin/vim", nil }

	command, err := editor.Resolve(context.Background(), "/home/u/.ssh/config")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if command.Binary != "/usr/bin/vim" {
		t.Errorf("Binary = %q", command.Binary)
	}
	want := []string{"-u", "NONE", "/home/u/.ssh/config"}
	if len(command.Args) != len(want) {
		t.Fatalf("Args = %v, want %v", command.Args, want)
	}
	for i, arg := range want {
		if command.Args[i] != arg {
			t.Errorf("Args[%d] = %q, want %q", i, command.Args[i], arg)
		}
	}
}

func TestConfigEditorRejectsEmptyPath(t *testing.T) {
	editor, err := NewConfigEditor("vim")
	if err != nil {
		t.Fatalf("NewConfigEditor: %v", err)
	}

	if _, err := editor.Resolve(context.Background(), "  "); !errors.Is(err, domain.ErrInvalidPath) {
		t.Fatalf("Resolve = %v, want ErrInvalidPath", err)
	}
}

func TestNewConfigEditorRejectsEmpty(t *testing.T) {
	if _, err := NewConfigEditor("   "); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("NewConfigEditor = %v, want ErrInvalidArgument", err)
	}
}

func TestParseCommand(t *testing.T) {
	command, err := parseCommand("  nano  -w  ")
	if err != nil {
		t.Fatalf("parseCommand: %v", err)
	}

	if command.Binary != "nano" {
		t.Errorf("Binary = %q", command.Binary)
	}
	if len(command.Args) != 1 || command.Args[0] != "-w" {
		t.Errorf("Args = %v", command.Args)
	}
}
