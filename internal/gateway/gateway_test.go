package gateway

import (
	"testing"
	"time"

	"github.com/golang/protobuf/ptypes"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/brocaar/lorawan"
	"github.com/brocaar/lorawan/band"
	"github.com/dev-ansh-r/loraserver/api/common"
	"github.com/dev-ansh-r/loraserver/api/gw"
	"github.com/dev-ansh-r/loraserver/internal/config"
	"github.com/dev-ansh-r/loraserver/internal/storage"
	"github.com/dev-ansh-r/loraserver/internal/test"
)

type GatewayConfigurationTestSuite struct {
	suite.Suite
	test.DatabaseTestSuiteBase

	backend *test.GatewayBackend
	gateway storage.Gateway
}

func (ts *GatewayConfigurationTestSuite) SetupSuite() {
	ts.DatabaseTestSuiteBase.SetupSuite()

	assert := require.New(ts.T())
	ts.backend = test.NewGatewayBackend()
	config.C.NetworkServer.Gateway.Backend.Backend = ts.backend
	ts.gateway = storage.Gateway{
		GatewayID: lorawan.EUI64{1, 2, 3, 4, 5, 6, 7, 8},
	}
	assert.NoError(storage.CreateGateway(ts.DB(), &ts.gateway))
}

func (ts *GatewayConfigurationTestSuite) TestUpdate() {
	ts.T().Run("No gateway-profile", func(t *testing.T) {
		assert := require.New(t)

		assert.NoError(handleConfigurationUpdate(ts.DB(), ts.gateway, ""))
		assert.Equal(0, len(ts.backend.GatewayConfigPacketChan))
	})

	ts.T().Run("With gateway-profile", func(t *testing.T) {
		assert := require.New(t)

		gp := storage.GatewayProfile{
			Channels: []int64{0, 1, 2},
			ExtraChannels: []storage.ExtraChannel{
				{
					Modulation:       string(band.LoRaModulation),
					Frequency:        867100000,
					Bandwidth:        125,
					SpreadingFactors: []int64{7, 8, 9, 10, 11, 12},
				},
				{
					Modulation: string(band.FSKModulation),
					Frequency:  868800000,
					Bandwidth:  125,
					Bitrate:    50000,
				},
			},
		}

		assert.NoError(storage.CreateGatewayProfile(ts.DB(), &gp))

		// to work around timestamp truncation
		var err error
		gp, err = storage.GetGatewayProfile(ts.DB(), gp.ID)
		assert.NoError(err)

		ts.gateway.GatewayProfileID = &gp.ID
		assert.NoError(storage.UpdateGateway(ts.DB(), &ts.gateway))

		assert.NoError(handleConfigurationUpdate(ts.DB(), ts.gateway, ""))

		gwConfig := <-ts.backend.GatewayConfigPacketChan
		assert.Equal(gw.GatewayConfiguration{
			Version:   gp.GetVersion(),
			GatewayId: ts.gateway.GatewayID[:],
			Channels: []*gw.ChannelConfiguration{
				{
					Frequency:  868100000,
					Modulation: common.Modulation_LORA,
					ModulationConfig: &gw.ChannelConfiguration_LoraModulationConfig{
						LoraModulationConfig: &gw.LoRaModulationConfig{
							Bandwidth:        125,
							SpreadingFactors: []uint32{7, 8, 9, 10, 11, 12},
						},
					},
				},
				{
					Frequency:  868300000,
					Modulation: common.Modulation_LORA,
					ModulationConfig: &gw.ChannelConfiguration_LoraModulationConfig{
						LoraModulationConfig: &gw.LoRaModulationConfig{
							Bandwidth:        125,
							SpreadingFactors: []uint32{7, 8, 9, 10, 11, 12},
						},
					},
				},
				{
					Frequency:  868500000,
					Modulation: common.Modulation_LORA,
					ModulationConfig: &gw.ChannelConfiguration_LoraModulationConfig{
						LoraModulationConfig: &gw.LoRaModulationConfig{
							Bandwidth:        125,
							SpreadingFactors: []uint32{7, 8, 9, 10, 11, 12},
						},
					},
				},
				{
					Frequency:  867100000,
					Modulation: common.Modulation_LORA,
					ModulationConfig: &gw.ChannelConfiguration_LoraModulationConfig{
						LoraModulationConfig: &gw.LoRaModulationConfig{
							Bandwidth:        125,
							SpreadingFactors: []uint32{7, 8, 9, 10, 11, 12},
						},
					},
				},
				{
					Frequency:  868800000,
					Modulation: common.Modulation_FSK,
					ModulationConfig: &gw.ChannelConfiguration_FskModulationConfig{
						FskModulationConfig: &gw.FSKModulationConfig{
							Bandwidth: 125,
							Bitrate:   50000,
						},
					},
				},
			},
		}, gwConfig)
	})
}

func TestGatewayConfigurationUpdate(t *testing.T) {
	suite.Run(t, new(GatewayConfigurationTestSuite))
}

type GatewayStatsTestSuite struct {
	suite.Suite
	test.DatabaseTestSuiteBase

	gateway storage.Gateway
}

func (ts *GatewayStatsTestSuite) SetupSuite() {
	assert := require.New(ts.T())
	ts.DatabaseTestSuiteBase.SetupSuite()

	ts.gateway = storage.Gateway{
		GatewayID: lorawan.EUI64{1, 2, 3, 4, 5, 6, 7, 8},
	}
	assert.NoError(storage.CreateGateway(ts.DB(), &ts.gateway))

	assert.NoError(storage.SetAggregationIntervals([]storage.AggregationInterval{
		storage.AggregationMinute,
	}))

	assert.NoError(storage.SetTimeLocation("Europe/Amsterdam"))

	storage.SetMetricsTTL(time.Minute, time.Minute, time.Minute, time.Minute)
}

func (ts *GatewayStatsTestSuite) TestStats() {
	assert := require.New(ts.T())

	loc, err := time.LoadLocation("Europe/Amsterdam")
	assert.NoError(err)

	now := time.Now().In(loc)
	stats := gw.GatewayStats{
		GatewayId: ts.gateway.GatewayID[:],
		Location: &common.Location{
			Latitude:  1.123,
			Longitude: 1.124,
			Altitude:  15.3,
		},
		RxPacketsReceived:   11,
		RxPacketsReceivedOk: 9,
		TxPacketsReceived:   13,
		TxPacketsEmitted:    10,
	}
	stats.Time, _ = ptypes.TimestampProto(now)

	assert.NoError(handleGatewayStats(ts.RedisPool(), stats))

	metrics, err := storage.GetMetrics(ts.RedisPool(), storage.AggregationMinute, "gw:0102030405060708", now, now)
	assert.NoError(err)
	assert.Equal([]storage.MetricsRecord{
		{
			Time: time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), now.Minute(), 0, 0, loc),
			Metrics: map[string]float64{
				"rx_count":    11,
				"rx_ok_count": 9,
				"tx_count":    13,
				"tx_ok_count": 10,
			},
		},
	}, metrics)
}

func TestGatewayStats(t *testing.T) {
	suite.Run(t, new(GatewayStatsTestSuite))
}
