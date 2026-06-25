package cmd

import (
	"fmt"
	"os"

	"github.com/patri/manifest-ref-scanner/internal/config"
	"github.com/patri/manifest-ref-scanner/internal/output"
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
)

var rootCmd = &cobra.Command{
	Use:   "manifest-ref-scanner [path]",
	Short: "Scan a Kubernetes GitOps repository for OCI artifact references",
	Args:  cobra.ExactArgs(1),
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

		w := os.Stdout
		if outputFile != "" {
			f, err := os.Create(outputFile)
			if err != nil {
				return err
			}
			defer f.Close()
			w = f
		}

		return output.WriteYAML(w, result.Artifacts)
	},
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
}
