package cmd

import (
	"fmt"
	"github.com/spf13/cobra"
	"os"
)

// Root command
var rootCmd = &cobra.Command{
	Use:   "ami-checker",
	Short: "A CLI application to filter AWS AMIs",
}

// Execute function to run the root command
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
