// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"

	"github.com/LazyGeniusMan/turutan/internal/config"
)

func TestStateCmdRegistered(t *testing.T) {
	resetRootFlags(t)
	isolateConfigCandidates(t)
	root := newRootCmd(viper.New())
	names := map[string]bool{}
	var stateSubs []string
	for _, sub := range root.Commands() {
		names[sub.Name()] = true
		if sub.Name() == "state" {
			for _, leaf := range sub.Commands() {
				stateSubs = append(stateSubs, leaf.Name())
			}
		}
	}
	for _, want := range []string{"state", "init", "check", "diff", "update", "registry", "version"} {
		if !names[want] {
			t.Errorf("subcommand %q missing from root", want)
		}
	}
	for _, want := range []string{"input", "sync"} {
		if !strings.Contains(strings.Join(stateSubs, ","), want) {
			t.Errorf("state subcommand %q missing (have %v)", want, stateSubs)
		}
	}
}

func TestStateHelp(t *testing.T) {
	t.Run("state lists input and sync", func(t *testing.T) {
		cmd := newStateCmd()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"--help"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("state --help error: %v", err)
		}
		for _, want := range []string{"input", "sync"} {
			if !strings.Contains(out.String(), want) {
				t.Errorf("help = %q, want it to list %q", out.String(), want)
			}
		}
	})
	t.Run("input help covers flags", func(t *testing.T) {
		cmd := newStateInputCmd()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"--help"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("state input --help error: %v", err)
		}
		for _, want := range []string{"data-file", "defaults", "force", "conflict", "preview"} {
			if !strings.Contains(out.String(), want) {
				t.Errorf("help = %q, want flag --%s", out.String(), want)
			}
		}
		if strings.Contains(out.String(), "--ref") {
			t.Errorf("help = %q, want no --ref on state input", out.String())
		}
	})
	t.Run("sync help covers flags without ref", func(t *testing.T) {
		cmd := newStateSyncCmd()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"--help"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("state sync --help error: %v", err)
		}
		for _, want := range []string{"force", "conflict", "preview"} {
			if !strings.Contains(out.String(), want) {
				t.Errorf("help = %q, want flag --%s", out.String(), want)
			}
		}
		if strings.Contains(out.String(), "--ref") {
			t.Errorf("help = %q, want no --ref on state sync", out.String())
		}
	})
}

func TestStateSyncCmd(t *testing.T) {
	oldCfg := appCfg
	appCfg.nonInteractive = true
	defer func() { appCfg = oldCfg }()

	t.Run("sync re-renders hand-edited inputs", func(t *testing.T) {
		_, regs, flag := makeCmdRegistry(t, "", map[string]string{
			"greet.txt.tmpl": "hi {{.project_name}}\n",
		})
		useCmdRegistryConfig(t, regs)
		dst := filepath.Join(t.TempDir(), "proj")
		var initOut bytes.Buffer
		initCmd := newInitCmd()
		initCmd.SetOut(&initOut)
		initCmd.SetErr(&bytes.Buffer{})
		initCmd.SetArgs([]string{"--template", flag, dst, "--defaults"})
		if err := initCmd.Execute(); err != nil {
			t.Fatalf("init error: %v", err)
		}
		state, err := config.LoadState(os.DirFS(dst))
		if err != nil {
			t.Fatal(err)
		}
		state.Data["project_name"] = "edited"
		if err := config.SaveState(dst, state); err != nil {
			t.Fatal(err)
		}
		runInProject(t, dst)
		cmd := newStateSyncCmd()
		var stdout bytes.Buffer
		cmd.SetOut(&stdout)
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("state sync error: %v", err)
		}
		if !strings.Contains(stdout.String(), "synced") {
			t.Errorf("stdout = %q, want the synced summary", stdout.String())
		}
		got, err := os.ReadFile(filepath.Join(dst, "greet.txt")) // #nosec G304 -- test-only fixture path under t.TempDir(), not external input
		if err != nil || string(got) != "hi edited\n" {
			t.Errorf("greet.txt = %q, want hand-edited re-render", got)
		}
	})
	t.Run("invalid conflict mode fails", func(t *testing.T) {
		_, dst, _ := initTestProject(t, map[string]string{"hello.txt": "v1\n"})
		runInProject(t, dst)
		cmd := newStateSyncCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"--conflict", "merge"})
		if err := cmd.Execute(); err == nil {
			t.Error("state sync with bad --conflict succeeded, want error")
		}
	})
	t.Run("input flags register", func(t *testing.T) {
		cmd := newStateInputCmd()
		for _, name := range []string{"data-file", "defaults", "conflict", "force", "preview"} {
			if cmd.Flags().Lookup(name) == nil {
				t.Errorf("flag --%s missing", name)
			}
		}
		if cmd.Flags().Lookup("ref") != nil {
			t.Error("flag --ref present on state input, want none")
		}
	})
	t.Run("sync flags register without ref", func(t *testing.T) {
		cmd := newStateSyncCmd()
		for _, name := range []string{"conflict", "force", "preview"} {
			if cmd.Flags().Lookup(name) == nil {
				t.Errorf("flag --%s missing", name)
			}
		}
		if cmd.Flags().Lookup("ref") != nil {
			t.Error("flag --ref present on state sync, want none")
		}
	})
}

func TestCreateCmdGone(t *testing.T) {
	resetRootFlags(t)
	isolateConfigCandidates(t)
	cmd := newRootCmd(viper.New())
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("help error: %v", err)
	}
	if strings.Contains(stdout.String(), "create") {
		t.Errorf("help mentions a create command, want it gone:\n%s", stdout.String())
	}
	for _, sub := range cmd.Commands() {
		if sub.Name() == "create" {
			t.Error("create subcommand still registered, want it removed")
		}
	}
}
