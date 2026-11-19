package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Aporixa/elenxa/benchmark"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "validate":
		err = validate(os.Args[2:])
	case "run":
		err = run(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "elenxa-bench:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: elenxa-bench validate -scenario FILE")
	fmt.Fprintln(os.Stderr, "       elenxa-bench run -scenario FILE [-out DIR]")
}

func validate(args []string) error {
	flags := flag.NewFlagSet("validate", flag.ContinueOnError)
	path := flags.String("scenario", "", "scenario JSON file")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *path == "" {
		return fmt.Errorf("-scenario is required")
	}
	scenario, err := benchmark.LoadScenario(*path)
	if err != nil {
		return err
	}
	fmt.Printf("valid scenario %q (%d trial(s))\n", scenario.ID, trialCount(scenario))
	return nil
}

func run(args []string) error {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	path := flags.String("scenario", "", "scenario JSON file")
	out := flags.String("out", "benchmark/results", "result directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *path == "" {
		return fmt.Errorf("-scenario is required")
	}
	scenario, err := benchmark.LoadScenario(*path)
	if err != nil {
		return err
	}
	runner, err := benchmark.NewRunner(scenario)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(*out, 0o750); err != nil {
		return err
	}
	allPassed := true
	for trial := 1; trial <= trialCount(scenario); trial++ {
		result := runner.Run(context.Background(), scenario, trial)
		filename := filepath.Join(*out, benchmark.ResultFilename(scenario.ID, trial, result.StartedAt))
		if err := writeResult(filename, result); err != nil {
			return err
		}
		fmt.Printf("trial %d: passed=%t task=%s result=%s\n", trial, result.Passed, result.TaskID, filename)
		allPassed = allPassed && result.Passed
	}
	if !allPassed {
		return fmt.Errorf("one or more trials failed")
	}
	return nil
}

func trialCount(s benchmark.Scenario) int {
	if s.Trials > 0 {
		return s.Trials
	}
	return 1
}

func writeResult(path string, result benchmark.Result) error {
	raw, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	temp, err := os.CreateTemp(filepath.Dir(path), ".elenxa-result-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(raw); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempName, path)
}
