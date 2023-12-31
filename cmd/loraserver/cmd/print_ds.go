package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/brocaar/lorawan"
	"github.com/dev-ansh-r/loraserver/internal/common"
	"github.com/dev-ansh-r/loraserver/internal/config"
	"github.com/dev-ansh-r/loraserver/internal/storage"

	log "github.com/sirupsen/logrus"

	"github.com/spf13/cobra"
)

var printDSCmd = &cobra.Command{
	Use:     "print-ds",
	Short:   "Print the device-session as JSON (for debugging)",
	Example: `loraserver print-ds 0102030405060708`,
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) != 1 {
			log.Fatalf("hex encoded DevEUI must be given as an argument")
		}

		config.C.Redis.Pool = common.NewRedisPool(
			config.C.Redis.URL,
			config.C.Redis.MaxIdle,
			config.C.Redis.IdleTimeout,
		)

		var devEUI lorawan.EUI64
		if err := devEUI.UnmarshalText([]byte(args[0])); err != nil {
			log.WithError(err).Fatal("decode DevEUI error")
		}

		ds, err := storage.GetDeviceSession(config.C.Redis.Pool, devEUI)
		if err != nil {
			log.WithError(err).Fatal("get device-session error")
		}

		b, err := json.MarshalIndent(ds, "", "    ")
		if err != nil {
			log.WithError(err).Fatal("json marshal error")
		}

		fmt.Println(string(b))
	},
}
