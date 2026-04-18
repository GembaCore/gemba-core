package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

func newVersionCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print build info",
		RunE: func(cmd *cobra.Command, args []string) error {
			info := map[string]string{
				"version": version,
				"commit":  commit,
				"date":    date,
			}
			if asJSON {
				b, _ := json.MarshalIndent(info, "", "  ")
				fmt.Println(string(b))
				return nil
			}
			fmt.Printf("bc %s (commit %s, built %s)\n",
				info["version"], info["commit"], info["date"])
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON")
	return cmd
}
