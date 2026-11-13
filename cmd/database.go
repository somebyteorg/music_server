package cmd

import (
	"fmt"

	"github.com/navidrome/navidrome/db"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/persistence"
	"github.com/spf13/cobra"
)

func init() {
	database := &cobra.Command{Use: "database", Aliases: []string{"db"}, Short: "Manage the PostgreSQL schema"}
	database.AddCommand(&cobra.Command{
		Use:   "migrate",
		Short: "Initialize or upgrade the PostgreSQL schema",
		Run: func(cmd *cobra.Command, _ []string) {
			defer db.Init(cmd.Context())()
			if err := persistence.New(db.Db()).Library().StoreMusicFolder(cmd.Context()); err != nil {
				log.Fatal("Could not initialize music library", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "PostgreSQL schema is up to date.")
		},
	})
	rootCmd.AddCommand(database)
}
