package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"

	"github.com/opentofu/registry-stable/internal/files"
	"github.com/opentofu/registry-stable/internal/github"
	"github.com/opentofu/registry-stable/internal/module"

	regaddr "github.com/opentofu/registry-address"
)

type Output struct {
	File       string `json:"file"`
	Namespace  string `json:"namespace"`
	Name       string `json:"name"`
	Target     string `json:"target"`
	Validation string `json:"validation"`
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	repository := flag.String("repository", "", "The module repository to add")
	outputFile := flag.String("output", "", "Path to write JSON result to")
	moduleDataDir := flag.String("module-data", "../modules", "Directory containing the module data")

	flag.Parse()

	ctx := context.Background()
	token, err := github.EnvAuthToken()
	if err != nil {
		logger.Error("Initialization Error", slog.Any("err", err))
		os.Exit(1)
	}
	ghClient := github.NewClient(ctx, logger, token)

	storage := module.NewStorage(*moduleDataDir, logger, ghClient)

	output := Output{}

	err = func() error {
		// Lower case input
		re := regexp.MustCompile("(?P<Namespace>[a-zA-Z0-9]+)/terraform-(?P<Target>[a-zA-Z0-9]*)-(?P<Name>[a-zA-Z0-9-]*)")
		match := re.FindStringSubmatch(*repository)
		if match == nil {
			return fmt.Errorf("Invalid repository name: %s", *repository)
		}

		submitted := storage.Create(module.Identifier{
			Namespace:    match[re.SubexpIndex("Namespace")],
			Name:         match[re.SubexpIndex("Name")],
			TargetSystem: match[re.SubexpIndex("Target")],
		})

		_, err = regaddr.ParseModuleSource(submitted.String())
		if err != nil {
			return err
		}

		modules, err := storage.List()
		if err != nil {
			return err
		}
		for _, p := range modules {
			if strings.ToLower(p.String()) == strings.ToLower(submitted.String()) {
				return fmt.Errorf("Repository already exists in the registry, %s", p.String())
			}
		}

		err = submitted.UpdateMetadata()
		if err != nil {
			return fmt.Errorf("An unexpected error occured: %w", err)
		}
		if len(submitted.Versions) == 0 {
			return fmt.Errorf("No versions detected for repository %s", submitted.Repository.URL())
		}

		err = storage.Save(submitted)
		if err != nil {
			return err
		}

		output.Namespace = submitted.Namespace
		output.Name = submitted.Name
		output.Target = submitted.TargetSystem
		output.File = storage.Path(submitted.Identifier)
		return nil
	}()

	if err != nil {
		logger.Error("Unable to add module", slog.Any("err", err))
		output.Validation = err.Error()
		// Don't exit yet, still need to write the json.
	}

	jsonErr := files.SafeWriteObjectToJSONFile(*outputFile, output)
	if jsonErr != nil {
		// This really should not happen
		panic(jsonErr)
	}

	if err != nil {
		os.Exit(1)
	}
}
