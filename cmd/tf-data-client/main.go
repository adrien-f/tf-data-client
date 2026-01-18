package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"

	tfclient "github.com/adrien-f/tf-data-client"
	"github.com/go-logr/logr"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// Parse command line flags
	providerArg := flag.String("provider", "", "Provider to use (e.g., hashicorp/kubernetes)")
	version := flag.String("version", "", "Provider version (optional, defaults to latest)")
	dataSource := flag.String("data-source", "", "Data source to read (e.g., kubernetes_all_namespaces)")
	configJSON := flag.String("config", "{}", "Provider configuration as JSON")
	dataConfigJSON := flag.String("data-config", "{}", "Data source configuration as JSON")
	output := flag.String("output", "", "Output file for JSON result (optional, defaults to stdout)")
	listDataSources := flag.Bool("list-data-sources", false, "List available data sources and exit")
	cacheDir := flag.String("cache-dir", "", "Provider cache directory (optional)")
	verbose := flag.Bool("verbose", false, "Enable verbose logging")

	flag.Parse()

	if *providerArg == "" {
		return fmt.Errorf("--provider is required")
	}

	// Parse provider argument (namespace/name)
	parts := strings.Split(*providerArg, "/")
	if len(parts) != 2 {
		return fmt.Errorf("provider must be in format namespace/name (e.g., hashicorp/kubernetes)")
	}
	namespace, name := parts[0], parts[1]

	// Create client with options
	var opts []tfclient.Option
	if *cacheDir != "" {
		opts = append(opts, tfclient.WithCacheDir(*cacheDir))
	}

	// Configure logging: slog -> logr -> library
	logLevel := slog.LevelInfo
	if *verbose {
		logLevel = slog.LevelDebug
	}
	slogHandler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel})
	logger := logr.FromSlogHandler(slogHandler)
	opts = append(opts, tfclient.WithLogger(logger))

	client, err := tfclient.New(opts...)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}
	defer client.Close()

	ctx := context.Background()

	// Create provider
	fmt.Fprintf(os.Stderr, "Creating provider %s/%s", namespace, name)
	if *version != "" {
		fmt.Fprintf(os.Stderr, "@%s", *version)
	}
	fmt.Fprintln(os.Stderr, "...")

	provider, err := client.CreateProvider(ctx, tfclient.ProviderConfig{
		Namespace: namespace,
		Name:      name,
		Version:   *version,
	})
	if err != nil {
		return fmt.Errorf("failed to create provider: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Provider %s/%s@%s ready\n", provider.Namespace, provider.Name, provider.Version)

	// List data sources if requested
	if *listDataSources {
		dataSources := provider.ListDataSources()
		fmt.Println("Available data sources:")
		for _, ds := range dataSources {
			fmt.Printf("  - %s\n", ds)
		}
		return nil
	}

	// Parse provider config
	var config map[string]interface{}
	if err := json.Unmarshal([]byte(*configJSON), &config); err != nil {
		return fmt.Errorf("failed to parse provider config JSON: %w", err)
	}

	// Configure provider
	fmt.Fprintf(os.Stderr, "Configuring provider...\n")
	if err := provider.Configure(ctx, config); err != nil {
		return fmt.Errorf("failed to configure provider: %w", err)
	}

	// If no data source specified, just exit
	if *dataSource == "" {
		fmt.Fprintf(os.Stderr, "Provider configured successfully. Use --data-source to read a data source.\n")
		return nil
	}

	// Parse data source config
	var dataConfig map[string]interface{}
	if err := json.Unmarshal([]byte(*dataConfigJSON), &dataConfig); err != nil {
		return fmt.Errorf("failed to parse data source config JSON: %w", err)
	}

	// Read data source
	fmt.Fprintf(os.Stderr, "Reading data source %s...\n", *dataSource)
	result, err := provider.ReadDataSource(ctx, *dataSource, dataConfig)
	if err != nil {
		return fmt.Errorf("failed to read data source: %w", err)
	}

	// Marshal result to JSON
	resultJSON, err := json.MarshalIndent(result.State, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal result to JSON: %w", err)
	}

	// Output result
	if *output != "" {
		if err := os.WriteFile(*output, resultJSON, 0644); err != nil {
			return fmt.Errorf("failed to write output file: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Result written to %s\n", *output)
	} else {
		fmt.Println(string(resultJSON))
	}

	return nil
}
