package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/patri/manifest-ref-scanner/internal/config"
	"github.com/patri/manifest-ref-scanner/internal/output"
	"github.com/patri/manifest-ref-scanner/internal/registry"
	"github.com/patri/manifest-ref-scanner/internal/scanner"
	"github.com/spf13/cobra"
)

var (
	cfgFile                string
	outputFile             string
	excludeGlobs           []string
	defaultNamespace       string
	disableHelm            bool
	disableKustomize       bool
	kustomizeOverlayFilter []string

	format           string
	formatConfigFile string
	rawArgs          []string
	excludeRefGlobs  []string
)

var rootCmd = &cobra.Command{
	Use:          "manifest-ref-scanner [path]",
	Short:        "Scan a Kubernetes GitOps repository for OCI artifact references",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := config.DefaultConfig()

		if cfgFile != "" {
			userCfg, err := config.Load(cfgFile)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			cfg = config.Merge(cfg, userCfg)
		}

		opts := scanner.Options{
			DefaultNamespace:       defaultNamespace,
			ExcludeGlobs:           excludeGlobs,
			DisableHelm:            disableHelm,
			DisableKustomize:       disableKustomize,
			KustomizeOverlayFilter: kustomizeOverlayFilter,
		}

		result, err := scanner.Scan(args[0], cfg, opts)
		if err != nil {
			return fmt.Errorf("scan: %w", err)
		}

		if len(excludeRefGlobs) > 0 {
			result.Artifacts = filterArtifacts(result.Artifacts, excludeRefGlobs)
		}

		w := os.Stdout
		if outputFile != "" {
			f, err := os.Create(outputFile)
			if err != nil {
				return err
			}
			defer f.Close()
			w = f
		}

		formatter, err := resolveFormatter(format, formatConfigFile, rawArgs, args[0])
		if err != nil {
			return err
		}

		return formatter.Format(w, result.Artifacts)
	},
}

// filterArtifacts drops artifacts whose Reference contains any of the given patterns.
func filterArtifacts(arts []*registry.Artifact, patterns []string) []*registry.Artifact {
	out := make([]*registry.Artifact, 0, len(arts))
	for _, a := range arts {
		excluded := false
		for _, p := range patterns {
			if strings.Contains(a.Reference, p) {
				excluded = true
				break
			}
		}
		if !excluded {
			out = append(out, a)
		}
	}
	return out
}

// resolveFormatter picks the right Formatter based on flags.
// Priority: --format-config (custom file) > --format (named built-in) > default YAML.
func resolveFormatter(format, formatConfigFile string, rawArgs []string, scanPath string) (output.Formatter, error) {
	templateArgs, err := parseArgs(rawArgs)
	if err != nil {
		return nil, err
	}

	if formatConfigFile != "" {
		cfg, err := output.Load(formatConfigFile)
		if err != nil {
			return nil, err
		}
		return output.NewTemplateFormatter(cfg, templateArgs, scanPath)
	}

	if format != "" && format != "yaml" {
		cfg, ok := output.BuiltinFormat(format)
		if !ok {
			return nil, fmt.Errorf("unknown --format %q; valid values: yaml, ocm, bom (or use --format-config for a custom template)", format)
		}
		return output.NewTemplateFormatter(cfg, templateArgs, scanPath)
	}

	return &output.YAMLFormatter{}, nil
}

// parseArgs splits "key=value" strings from --arg flags into a map.
func parseArgs(raw []string) (map[string]string, error) {
	m := make(map[string]string, len(raw))
	for _, s := range raw {
		k, v, ok := strings.Cut(s, "=")
		if !ok {
			return nil, fmt.Errorf("--arg %q: expected key=value format", s)
		}
		m[k] = v
	}
	return m, nil
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.Flags().StringVarP(&cfgFile, "config", "c", "", "path to refs config YAML file (appended to built-ins)")
	rootCmd.Flags().StringVarP(&outputFile, "output", "o", "", "write output to file (default: stdout)")
	rootCmd.Flags().StringArrayVarP(&excludeGlobs, "exclude", "e", nil, "glob pattern to exclude (repeatable); matched against dir name and path relative to root")
	rootCmd.Flags().StringVar(&defaultNamespace, "default-namespace", "default", "namespace used as last-resort fallback when a resource reference omits namespace")
	rootCmd.Flags().BoolVar(&disableHelm, "no-helm", false, "disable Helm chart rendering; chart directories are skipped silently")
	rootCmd.Flags().BoolVar(&disableKustomize, "no-kustomize", false, "disable kustomize overlay rendering; process files as plain YAML")
	rootCmd.Flags().StringArrayVar(&kustomizeOverlayFilter, "kustomize-overlay", nil, "render only kustomize overlays whose path matches this glob (repeatable); others are skipped")

	rootCmd.Flags().StringVarP(&format, "format", "f", "yaml", `output format: yaml (default), ocm, bom`)
	rootCmd.Flags().StringVar(&formatConfigFile, "format-config", "", "path to a custom output format config YAML file (overrides --format)")
	rootCmd.Flags().StringArrayVar(&rawArgs, "arg", nil, "template argument as key=value (repeatable); overrides args defined in --format-config or built-in defaults")
	rootCmd.Flags().StringArrayVar(&excludeRefGlobs, "exclude-ref", nil, "exclude artifacts whose reference contains this substring (repeatable)")
}
