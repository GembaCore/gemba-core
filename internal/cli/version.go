package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

func newVersionCmd(b BuildInfo) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print build info",
		RunE: func(cmd *cobra.Command, args []string) error {
			info := map[string]string{
				"version": b.Version,
				"commit":  b.Commit,
				"date":    b.Date,
			}
			if asJSON {
				out, _ := json.MarshalIndent(info, "", "  ")
				fmt.Println(string(out))
				return nil
			}
			fmt.Printf("gemba %s (commit %s, built %s)\n",
				info["version"], info["commit"], info["date"])
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON")
	return cmd
}
