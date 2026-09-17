package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type actionFile struct {
	Inputs map[string]struct {
		Description string `yaml:"description"`
		Default     string `yaml:"default"`
		Required    bool   `yaml:"required"`
	} `yaml:"inputs"`
	Runs struct {
		Using string       `yaml:"using"`
		Steps []actionStep `yaml:"steps"`
	} `yaml:"runs"`
}

type actionStep struct {
	Name string            `yaml:"name"`
	Run  string            `yaml:"run"`
	Env  map[string]string `yaml:"env"`
}

func readAction(t *testing.T) actionFile {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoDir, "action.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var action actionFile
	if err := yaml.Unmarshal(data, &action); err != nil {
		t.Fatal(err)
	}
	return action
}

func lintStep(t *testing.T, action actionFile) actionStep {
	t.Helper()
	for _, step := range action.Runs.Steps {
		if strings.Contains(step.Run, "tenet --base") {
			return step
		}
	}
	t.Fatal("no step lints the branch")
	return actionStep{}
}

func TestActionAnnotatesByDefault(t *testing.T) {
	action := readAction(t)
	if action.Runs.Using != "composite" {
		t.Errorf("the action runs %q", action.Runs.Using)
	}
	if got := action.Inputs["annotate"].Default; got != "true" {
		t.Errorf("annotate defaults to %q, want true", got)
	}
	lint := lintStep(t, action)
	if !strings.Contains(lint.Run, "format=github") {
		t.Errorf("the lint never asks for annotations:\n%s", lint.Run)
	}
}

func TestActionLintsThePullRequestText(t *testing.T) {
	lint := lintStep(t, readAction(t))
	if !strings.Contains(lint.Run, "--pr-text") {
		t.Errorf("the lint leaves the pull request text unjudged:\n%s", lint.Run)
	}
	if strings.Index(lint.Run, "--base") > strings.Index(lint.Run, "--pr-text") {
		t.Errorf("the text is judged before the diff:\n%s", lint.Run)
	}
	// A title is written by whoever opened the pull request, so expanding one
	// into the script would let them write the script.
	for _, name := range []string{"PR_TITLE", "PR_BODY"} {
		if !strings.Contains(lint.Env[name], "github.event.pull_request") {
			t.Errorf("%s is %q", name, lint.Env[name])
		}
		if !strings.Contains(lint.Run, "$"+name) {
			t.Errorf("the script does not read %s:\n%s", name, lint.Run)
		}
	}
	if strings.Contains(lint.Run, "${{") {
		t.Errorf("the script expands an input into itself:\n%s", lint.Run)
	}
}
