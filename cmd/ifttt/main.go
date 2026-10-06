package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"ifttt/internal/lint"
	"ifttt/internal/report"
)

var version = "dev"

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

type event struct {
	PullRequest *struct {
		Body string `json:"body"`
		Base struct {
			SHA string `json:"sha"`
		} `json:"base"`
		Head struct {
			SHA string `json:"sha"`
		} `json:"head"`
	} `json:"pull_request"`
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && (args[0] == "--version" || args[0] == "version") {
		fmt.Fprintln(stdout, "ifttt "+version)
		return 0
	}
	if len(args) == 0 || args[0] == "--help" || args[0] == "help" {
		fmt.Fprintln(stdout, "Usage: ifttt check --base <ref> [--head <ref>] [--fail-on-findings] [--github]\nChecks committed changes against the merge base. In Actions, base/head/body default to the PR event.")
		return 0
	}
	if args[0] != "check" {
		fmt.Fprintln(stderr, "ifttt: expected 'check'; use --help for usage")
		return 3
	}
	flags := flag.NewFlagSet("check", flag.ContinueOnError)
	flags.SetOutput(stderr)
	base := flags.String("base", "", "base ref (default: PR base SHA)")
	head := flags.String("head", "", "head ref (default: PR head SHA or HEAD)")
	repo := flags.String("repo", ".", "repository directory")
	fail := flags.Bool("fail-on-findings", false, "exit 1 for unoverridden coupling findings")
	github := flags.Bool("github", os.Getenv("GITHUB_ACTIONS") == "true", "emit GitHub workflow annotations")
	eventPath := flags.String("event", os.Getenv("GITHUB_EVENT_PATH"), "PR event JSON path; empty disables event loading")
	summary := flags.String("summary", os.Getenv("GITHUB_STEP_SUMMARY"), "append a Markdown job summary to this file")
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 3
	}
	failure := func(err error) int {
		fmt.Fprintf(stderr, "ifttt: %s\n", err)
		return 3
	}
	if flags.NArg() != 0 {
		return failure(fmt.Errorf("unexpected arguments: %v", flags.Args()))
	}
	var reason string
	if *eventPath != "" {
		data, err := os.ReadFile(*eventPath)
		if err != nil {
			return failure(fmt.Errorf("read event: %w", err))
		}
		var e event
		if err := json.Unmarshal(data, &e); err != nil {
			return failure(fmt.Errorf("parse event: %w", err))
		}
		if e.PullRequest == nil {
			return failure(fmt.Errorf("event has no pull_request; use --event= for an explicit local check"))
		}
		if *base == "" {
			*base = e.PullRequest.Base.SHA
		}
		if *head == "" {
			if e.PullRequest.Head.SHA == "" {
				return failure(fmt.Errorf("PR event is missing head.sha"))
			}
			*head = e.PullRequest.Head.SHA
		}
		reason = report.Override(e.PullRequest.Body)
	}
	result, err := lint.Check(lint.Options{Dir: *repo, Base: *base, Head: *head})
	if err != nil {
		return failure(err)
	}
	if err := report.Write(stdout, result, reason, *github); err != nil {
		return failure(err)
	}
	if *summary != "" {
		file, err := os.OpenFile(*summary, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
		if err != nil {
			return failure(fmt.Errorf("open summary: %w", err))
		}
		writeErr := report.Summary(file, result, reason)
		closeErr := file.Close()
		if writeErr != nil {
			return failure(fmt.Errorf("write summary: %w", writeErr))
		}
		if closeErr != nil {
			return failure(closeErr)
		}
	}
	return report.ExitCode(result, reason, *fail)
}
