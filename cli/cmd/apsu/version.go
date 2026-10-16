package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// Version is populated at build time via -ldflags="-X main.Version=..."
var Version = "1.4.1"

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print Apsu CLI version",
	Run: func(cmd *cobra.Command, args []string) {
		ver := strings.TrimPrefix(Version, "v")
		fmt.Printf("Apsu v%s\n", ver)
	},
}

func init() {
	cleanVer := strings.TrimPrefix(Version, "v")
	rootCmd.Version = cleanVer
	rootCmd.SetVersionTemplate("Apsu v{{.Version}}\n")
	rootCmd.AddCommand(versionCmd)
}
