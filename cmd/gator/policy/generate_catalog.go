package policy

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/policy/catalog"
	"github.com/spf13/cobra"
)

const generateCatalogExamples = `# Generate catalog from gatekeeper-library
gator policy generate-catalog --library-path=/path/to/gatekeeper-library

# Generate with custom output path
gator policy generate-catalog --library-path=. --output=catalog.yaml

# Generate with bundles file
gator policy generate-catalog --library-path=. --bundles=bundles.yaml

# Generate with custom version
gator policy generate-catalog --library-path=. --version=v1.2.0

# Generate with URLs instead of local paths (for publishing)
gator policy generate-catalog --library-path=. --base-url=https://raw.githubusercontent.com/open-policy-agent/gatekeeper-library/master`

func newGenerateCatalogCommand() *cobra.Command {
	var (
		libraryPath    string
		outputPath     string
		catalogName    string
		catalogVersion string
		repository     string
		bundlesFile    string
		baseURL        string
		validate       bool
	)

	cmd := &cobra.Command{
		Use:     "generate-catalog",
		Short:   "Generate a policy catalog from gatekeeper-library",
		Long:    `Scans a gatekeeper-library directory structure and generates a catalog.yaml file containing all discovered policies.`,
		Example: generateCatalogExamples,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGenerateCatalog(&generateCatalogOptions{
				libraryPath:    libraryPath,
				outputPath:     outputPath,
				catalogName:    catalogName,
				catalogVersion: catalogVersion,
				repository:     repository,
				bundlesFile:    bundlesFile,
				baseURL:        baseURL,
				validate:       validate,
			})
		},
	}

	cmd.Flags().StringVar(&libraryPath, "library-path", ".", "Path to the gatekeeper-library repository root")
	cmd.Flags().StringVarP(&outputPath, "output", "o", "catalog.yaml", "Output path for the generated catalog")
	cmd.Flags().StringVar(&catalogName, "name", "gatekeeper-library", "Name of the catalog")
	cmd.Flags().StringVar(&catalogVersion, "version", "v1.0.0", "Version of the catalog")
	cmd.Flags().StringVar(&repository, "repository", "https://github.com/open-policy-agent/gatekeeper-library", "Repository URL")
	cmd.Flags().StringVar(&bundlesFile, "bundles", "", "Path to bundles definition file (optional)")
	cmd.Flags().StringVar(&baseURL, "base-url", "", "Base URL for template/constraint paths (e.g., https://raw.githubusercontent.com/open-policy-agent/gatekeeper-library/master)")
	cmd.Flags().BoolVar(&validate, "validate", true, "Validate the generated catalog")

	return cmd
}

type generateCatalogOptions struct {
	libraryPath    string
	outputPath     string
	catalogName    string
	catalogVersion string
	repository     string
	bundlesFile    string
	baseURL        string
	validate       bool
}

func runGenerateCatalog(opts *generateCatalogOptions) error {
	// Resolve absolute path
	absPath, err := filepath.Abs(opts.libraryPath)
	if err != nil {
		return fmt.Errorf("resolving library path: %w", err)
	}

	// Verify library path exists
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return fmt.Errorf("library path does not exist: %s", absPath)
	}

	fmt.Fprintf(os.Stderr, "Scanning library at %s...\n", absPath)

	// Generate catalog
	cat, err := catalog.GenerateCatalog(&catalog.GeneratorOptions{
		LibraryPath:    absPath,
		CatalogName:    opts.catalogName,
		CatalogVersion: opts.catalogVersion,
		Repository:     opts.repository,
		BundlesFile:    opts.bundlesFile,
		BaseURL:        opts.baseURL,
	})
	if err != nil {
		return fmt.Errorf("generating catalog: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Found %d policies", len(cat.Policies))
	if len(cat.Bundles) > 0 {
		fmt.Fprintf(os.Stderr, " and %d bundles", len(cat.Bundles))
	}
	fmt.Fprintln(os.Stderr)

	// Validate if requested
	if opts.validate {
		if err := catalog.ValidateCatalogSchema(cat); err != nil {
			return fmt.Errorf("catalog validation failed: %w", err)
		}
		fmt.Fprintln(os.Stderr, "Catalog validation passed")
	}

	// Write catalog
	if err := catalog.WriteCatalog(cat, opts.outputPath); err != nil {
		return fmt.Errorf("writing catalog: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Catalog written to %s\n", opts.outputPath)
	return nil
}
