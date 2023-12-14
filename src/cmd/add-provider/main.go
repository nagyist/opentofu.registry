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
	"github.com/opentofu/registry-stable/internal/provider"

	regaddr "github.com/opentofu/registry-address"
)

type Output struct {
	File       string `json:"file"`
	Namespace  string `json:"namespace"`
	Name       string `json:"name"`
	Validation string `json:"validation"`
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	repository := flag.String("repository", "", "The provider repository to add")
	outputFile := flag.String("output", "", "Path to write JSON result to")
	providerDataDir := flag.String("provider-data", "../providers", "Directory containing the provider data")

	flag.Parse()

	ctx := context.Background()
	token, err := github.EnvAuthToken()
	if err != nil {
		logger.Error("Initialization Error", slog.Any("err", err))
		os.Exit(1)
	}
	ghClient := github.NewClient(ctx, logger, token)

	storage := provider.NewStorage(*providerDataDir, logger, ghClient)

	output := Output{}

	err = func() error {
		// Lower case input
		re := regexp.MustCompile("(?P<Namespace>[a-zA-Z0-9]+)/terraform-provider-(?P<Name>[a-zA-Z0-9-]*)")
		match := re.FindStringSubmatch(strings.ToLower(*repository))
		if match == nil {
			return fmt.Errorf("Invalid repository name: %s", *repository)
		}

		submitted := storage.Create(provider.Identifier{
			Namespace:    match[re.SubexpIndex("Namespace")],
			ProviderName: match[re.SubexpIndex("Name")],
		})

		_, err = regaddr.ParseProviderSource(submitted.String())
		if err != nil {
			return err
		}

		providers, err := storage.List()
		if err != nil {
			return err
		}
		for _, p := range providers {
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
			return fmt.Errorf("An unexpected error occured: %w", err)
		}

		output.Namespace = submitted.Namespace
		output.Name = submitted.ProviderName
		output.File = storage.Path(submitted.Identifier)
		return nil
	}()

	if err != nil {
		logger.Error("Unable to add provider", slog.Any("err", err))
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
