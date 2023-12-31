package config

import (
	"time"

	"github.com/gomodule/redigo/redis"

	"github.com/dev-ansh-r/loraserver/api/geo"
	"github.com/dev-ansh-r/loraserver/api/nc"
	"github.com/dev-ansh-r/loraserver/internal/api/client/asclient"
	"github.com/dev-ansh-r/loraserver/internal/backend"
	"github.com/dev-ansh-r/loraserver/internal/backend/gateway/gcppubsub"
	"github.com/dev-ansh-r/loraserver/internal/backend/gateway/mqtt"
	"github.com/dev-ansh-r/loraserver/internal/common"
	"github.com/dev-ansh-r/loraserver/internal/joinserver"
	"github.com/brocaar/lorawan"
	"github.com/brocaar/lorawan/band"
)

// Version defines the LoRa Server version.
var Version string

// Config defines the configuration structure.
type Config struct {
	General struct {
		LogLevel int `mapstructure:"log_level"`
	}

	PostgreSQL struct {
		DSN         string `mapstructure:"dsn"`
		Automigrate bool
		DB          *common.DBLogger
	} `mapstructure:"postgresql"`

	Redis struct {
		URL         string        `mapstructure:"url"`
		MaxIdle     int           `mapstructure:"max_idle"`
		IdleTimeout time.Duration `mapstructure:"idle_timeout"`
		Pool        *redis.Pool
	}

	NetworkServer struct {
		NetID                lorawan.NetID
		NetIDString          string        `mapstructure:"net_id"`
		DeduplicationDelay   time.Duration `mapstructure:"deduplication_delay"`
		DeviceSessionTTL     time.Duration `mapstructure:"device_session_ttl"`
		GetDownlinkDataDelay time.Duration `mapstructure:"get_downlink_data_delay"`

		Band struct {
			Band               band.Band
			Name               band.Name
			DwellTime400ms     bool `mapstructure:"dwell_time_400ms"`
			RepeaterCompatible bool `mapstructure:"repeater_compatible"`
		}

		NetworkSettings struct {
			InstallationMargin    float64 `mapstructure:"installation_margin"`
			RXWindow              int     `mapstructure:"rx_window"`
			RX1Delay              int     `mapstructure:"rx1_delay"`
			RX1DROffset           int     `mapstructure:"rx1_dr_offset"`
			RX2DR                 int     `mapstructure:"rx2_dr"`
			RX2Frequency          int     `mapstructure:"rx2_frequency"`
			DownlinkTXPower       int     `mapstructure:"downlink_tx_power"`
			EnabledUplinkChannels []int   `mapstructure:"enabled_uplink_channels"`
			DisableMACCommands    bool    `mapstructure:"disable_mac_commands"`
			DisableADR            bool    `mapstructure:"disable_adr"`

			ExtraChannels []struct {
				Frequency int
				MinDR     int `mapstructure:"min_dr"`
				MaxDR     int `mapstructure:"max_dr"`
			} `mapstructure:"extra_channels"`

			ClassB struct {
				PingSlotDR        int `mapstructure:"ping_slot_dr"`
				PingSlotFrequency int `mapstructure:"ping_slot_frequency"`
			} `mapstructure:"class_b"`

			RejoinRequest struct {
				Enabled   bool `mapstructure:"enabled"`
				MaxCountN int  `mapstructure:"max_count_n"`
				MaxTimeN  int  `mapstructure:"max_time_n"`
			} `mapstructure:"rejoin_request"`
		} `mapstructure:"network_settings"`

		Scheduler struct {
			SchedulerInterval time.Duration `mapstructure:"scheduler_interval"`

			ClassC struct {
				DownlinkLockDuration time.Duration `mapstructure:"downlink_lock_duration"`
			} `mapstructure:"class_c"`
		} `mapstructure:"scheduler"`

		API struct {
			Bind    string
			CACert  string `mapstructure:"ca_cert"`
			TLSCert string `mapstructure:"tls_cert"`
			TLSKey  string `mapstructure:"tls_key"`
		} `mapstructure:"api"`

		Gateway struct {
			// Deprecated
			Stats struct {
				Timezone string
			}

			Backend struct {
				Type      string           `mapstructure:"type"`
				Backend   backend.Gateway  `mapstructure:"-"`
				MQTT      mqtt.Config      `mapstructure:"mqtt"`
				GCPPubSub gcppubsub.Config `mapstructure:"gcp_pub_sub"`
			}
		}
	} `mapstructure:"network_server"`

	GeolocationServer struct {
		Client  geo.GeolocationServerServiceClient `mapstructure:"-"`
		Server  string                             `mapstructure:"server"`
		CACert  string                             `mapstructure:"ca_cert"`
		TLSCert string                             `mapstructure:"tls_cert"`
		TLSKey  string                             `mapstructure:"tls_key"`
	} `mapstructure:"geolocation_server"`

	JoinServer joinserver.Config `mapstructure:"join_server"`

	ApplicationServer struct {
		Pool asclient.Pool
	}

	NetworkController struct {
		Client nc.NetworkControllerServiceClient

		Server  string
		CACert  string `mapstructure:"ca_cert"`
		TLSCert string `mapstructure:"tls_cert"`
		TLSKey  string `mapstructure:"tls_key"`
	} `mapstructure:"network_controller"`

	Metrics struct {
		Timezone string `mapstructure:"timezone"`
		Redis    struct {
			AggregationIntervals []string      `mapstructure:"aggregation_intervals"`
			MinuteAggregationTTL time.Duration `mapstructure:"minute_aggregation_ttl"`
			HourAggregationTTL   time.Duration `mapstructure:"hour_aggregation_ttl"`
			DayAggregationTTL    time.Duration `mapstructure:"day_aggregation_ttl"`
			MonthAggregationTTL  time.Duration `mapstructure:"month_aggregation_ttl"`
		} `mapstructure:"redis"`
	} `mapstructure:"metrics"`
}

// SpreadFactorToRequiredSNRTable contains the required SNR to demodulate a
// LoRa frame for the given spreadfactor.
// These values are taken from the SX1276 datasheet.
var SpreadFactorToRequiredSNRTable = map[int]float64{
	6:  -5,
	7:  -7.5,
	8:  -10,
	9:  -12.5,
	10: -15,
	11: -17.5,
	12: -20,
}

// C holds the global configuration.
var C Config

// SchedulerBatchSize contains the batch size of the Class-C scheduler
var SchedulerBatchSize = 100

// ClassBEnqueueMargin contains the margin duration when scheduling Class-B
// messages.
var ClassBEnqueueMargin = time.Second * 5

// MulticastClassCInterval defines the interval between the gateway scheduling
// for Class-C multicast.
var MulticastClassCInterval = time.Second
