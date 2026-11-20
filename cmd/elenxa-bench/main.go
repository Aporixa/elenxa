package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Aporixa/elenxa/benchmark"
	"github.com/Aporixa/elenxa/benchmark/importers/promptfoo"
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
	case "import":
		err = importCases(os.Args[2:])
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
	fmt.Fprintln(os.Stderr, "       elenxa-bench import promptfoo -input FILE -template SCENARIO [-out DIR] [-dry-run]")
}

func importCases(args []string) error {
	if len(args) == 0 || args[0] != "promptfoo" {
		return fmt.Errorf("import requires the format promptfoo")
	}
	flags := flag.NewFlagSet("import promptfoo", flag.ContinueOnError)
	inputPath := flags.String("input", "", "frozen Promptfoo JSON or YAML")
	templatePath := flags.String("template", "", "native Elenxa lab scenario template")
	out := flags.String("out", "benchmark/scenarios/imported", "output directory")
	dryRun := flags.Bool("dry-run", false, "validate and report without writing")
	sourceVersion := flags.String("source-version", "", "pinned Promptfoo version or upstream revision")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if *inputPath == "" || *templatePath == "" {
		return fmt.Errorf("-input and -template are required")
	}
	template, err := benchmark.LoadScenario(*templatePath)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(*inputPath)
	if err != nil {
		return err
	}
	cases, err := promptfoo.Import(raw, template, promptfoo.Options{SourcePath: *inputPath, SourceVersion: *sourceVersion})
	if err != nil {
		return err
	}
	seenIDs := make(map[string]struct{}, len(cases))
	for _, imported := range cases {
		if err := benchmark.ValidateScenario(imported.Scenario); err != nil {
			return fmt.Errorf("converted case %s: %w", imported.Scenario.ID, err)
		}
		if _, exists := seenIDs[imported.Scenario.ID]; exists {
			return fmt.Errorf("converted cases produced duplicate scenario id %q", imported.Scenario.ID)
		}
		seenIDs[imported.Scenario.ID] = struct{}{}
	}
	for _, imported := range cases {
		fmt.Printf("case %s: diagnostics=%d", imported.Scenario.ID, len(imported.Diagnostics))
		if !*dryRun {
			if err := os.MkdirAll(*out, 0o750); err != nil {
				return err
			}
			path := filepath.Join(*out, imported.Scenario.ID+".json")
			if err := writeScenario(path, imported.Scenario); err != nil {
				return err
			}
			fmt.Printf(" output=%s", path)
		}
		fmt.Println()
		for _, diagnostic := range imported.Diagnostics {
			fmt.Printf("  - %s\n", diagnostic)
		}
	}
	fmt.Printf("converted %d frozen case(s); autonomous tasks are not started by import\n", len(cases))
	return nil
}

func writeScenario(path string, scenario benchmark.Scenario) error {
	raw, err := json.MarshalIndent(scenario, "", "  ")
	if err != nil {
		return err
	}
	return writePrivateFile(path, append(raw, '\n'))
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
	return writePrivateFile(path, raw)
}

func writePrivateFile(path string, raw []byte) error {
	temp, err := os.CreateTemp(filepath.Dir(path), ".elenxa-output-*")
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
