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
	cfgFile    string
	outputFile string
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

		result, err := scanner.Scan(args[0], cfg)
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
}
