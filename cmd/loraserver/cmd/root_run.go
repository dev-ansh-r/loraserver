package cmd

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	grpc_logrus "github.com/grpc-ecosystem/go-grpc-middleware/logging/logrus"
	grpc_ctxtags "github.com/grpc-ecosystem/go-grpc-middleware/tags"
	"github.com/pkg/errors"
	migrate "github.com/rubenv/sql-migrate"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/brocaar/lorawan"
	"github.com/brocaar/lorawan/band"
	"github.com/dev-ansh-r/loraserver/api/geo"
	"github.com/dev-ansh-r/loraserver/api/nc"
	"github.com/dev-ansh-r/loraserver/api/ns"
	"github.com/dev-ansh-r/loraserver/internal/api"
	"github.com/dev-ansh-r/loraserver/internal/api/client/asclient"
	"github.com/dev-ansh-r/loraserver/internal/backend"
	"github.com/dev-ansh-r/loraserver/internal/backend/controller"
	"github.com/dev-ansh-r/loraserver/internal/backend/gateway/gcppubsub"
	"github.com/dev-ansh-r/loraserver/internal/backend/gateway/mqtt"
	"github.com/dev-ansh-r/loraserver/internal/common"
	"github.com/dev-ansh-r/loraserver/internal/config"
	"github.com/dev-ansh-r/loraserver/internal/downlink"
	"github.com/dev-ansh-r/loraserver/internal/gateway"
	"github.com/dev-ansh-r/loraserver/internal/joinserver"
	"github.com/dev-ansh-r/loraserver/internal/migrations/code"
	"github.com/dev-ansh-r/loraserver/internal/storage"
	"github.com/dev-ansh-r/loraserver/internal/uplink"
)

func run(cmd *cobra.Command, args []string) error {
	var server = new(uplink.Server)
	var gwStats = new(gateway.StatsHandler)

	tasks := []func() error{
		setLogLevel,
		setBandConfig,
		setRXParameters,
		setupMetrics,
		printStartMessage,
		enableUplinkChannels,
		setRedisPool,
		setPostgreSQLConnection,
		setGatewayBackend,
		setApplicationServer,
		setGeolocationServer,
		setJoinServer,
		setNetworkController,
		runDatabaseMigrations,
		fixV2RedisCache,
		migrateGatewayStats,
		startAPIServer,
		startLoRaServer(server),
		startStatsServer(gwStats),
		startQueueScheduler,
	}

	for _, t := range tasks {
		if err := t(); err != nil {
			log.Fatal(err)
		}
	}

	sigChan := make(chan os.Signal)
	exitChan := make(chan struct{})
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	log.WithField("signal", <-sigChan).Info("signal received")
	go func() {
		log.Warning("stopping loraserver")
		if err := server.Stop(); err != nil {
			log.Fatal(err)
		}
		if err := gwStats.Stop(); err != nil {
			log.Fatal(err)
		}
		exitChan <- struct{}{}
	}()
	select {
	case <-exitChan:
	case s := <-sigChan:
		log.WithField("signal", s).Info("signal received, stopping immediately")
	}

	return nil
}

func setBandConfig() error {
	if config.C.NetworkServer.Band.Name == "" {
		return fmt.Errorf("band is undefined, valid options are: %s", strings.Join(bands, ", "))
	}
	dwellTime := lorawan.DwellTimeNoLimit
	if config.C.NetworkServer.Band.DwellTime400ms {
		dwellTime = lorawan.DwellTime400ms
	}
	bandConfig, err := band.GetConfig(config.C.NetworkServer.Band.Name, config.C.NetworkServer.Band.RepeaterCompatible, dwellTime)
	if err != nil {
		return errors.Wrap(err, "get band config error")
	}
	for _, c := range config.C.NetworkServer.NetworkSettings.ExtraChannels {
		if err := bandConfig.AddChannel(c.Frequency, c.MinDR, c.MaxDR); err != nil {
			return errors.Wrap(err, "add channel error")
		}
	}

	config.C.NetworkServer.Band.Band = bandConfig

	return nil
}

func setRXParameters() error {
	defaults := config.C.NetworkServer.Band.Band.GetDefaults()

	if config.C.NetworkServer.NetworkSettings.RX2DR == -1 {
		config.C.NetworkServer.NetworkSettings.RX2DR = defaults.RX2DataRate
	}

	if config.C.NetworkServer.NetworkSettings.RX2Frequency == -1 {
		config.C.NetworkServer.NetworkSettings.RX2Frequency = defaults.RX2Frequency
	}

	return nil
}

func setupMetrics() error {
	// setup aggregation intervals
	var intervals []storage.AggregationInterval
	for _, agg := range config.C.Metrics.Redis.AggregationIntervals {
		intervals = append(intervals, storage.AggregationInterval(strings.ToUpper(agg)))
	}
	if err := storage.SetAggregationIntervals(intervals); err != nil {
		return errors.Wrap(err, "set aggregation intervals error")
	}

	// setup timezone
	var err error
	if config.C.Metrics.Timezone == "" {
		err = storage.SetTimeLocation(config.C.NetworkServer.Gateway.Stats.Timezone)
	} else {
		err = storage.SetTimeLocation(config.C.Metrics.Timezone)
	}
	if err != nil {
		return errors.Wrap(err, "set time location error")
	}

	// setup storage TTL
	storage.SetMetricsTTL(
		config.C.Metrics.Redis.MinuteAggregationTTL,
		config.C.Metrics.Redis.HourAggregationTTL,
		config.C.Metrics.Redis.DayAggregationTTL,
		config.C.Metrics.Redis.MonthAggregationTTL,
	)

	return nil
}

func setLogLevel() error {
	log.SetLevel(log.Level(uint8(config.C.General.LogLevel)))
	return nil
}

func printStartMessage() error {
	log.WithFields(log.Fields{
		"version": version,
		"net_id":  config.C.NetworkServer.NetID.String(),
		"band":    config.C.NetworkServer.Band.Name,
		"docs":    "https:/www.loraserver.io/",
	}).Info("starting LoRa Server")
	return nil
}

func enableUplinkChannels() error {
	if len(config.C.NetworkServer.NetworkSettings.EnabledUplinkChannels) == 0 {
		return nil
	}

	log.Info("disabling all channels")
	for _, c := range config.C.NetworkServer.Band.Band.GetEnabledUplinkChannelIndices() {
		if err := config.C.NetworkServer.Band.Band.DisableUplinkChannelIndex(c); err != nil {
			return errors.Wrap(err, "disable uplink channel error")
		}
	}

	log.WithField("channels", config.C.NetworkServer.NetworkSettings.EnabledUplinkChannels).Info("enabling channels")
	for _, c := range config.C.NetworkServer.NetworkSettings.EnabledUplinkChannels {
		if err := config.C.NetworkServer.Band.Band.EnableUplinkChannelIndex(c); err != nil {
			errors.Wrap(err, "enable uplink channel error")
		}
	}

	return nil
}

func setRedisPool() error {
	log.WithField("url", config.C.Redis.URL).Info("setup redis connection pool")
	config.C.Redis.Pool = common.NewRedisPool(
		config.C.Redis.URL,
		config.C.Redis.MaxIdle,
		config.C.Redis.IdleTimeout,
	)
	return nil
}

func setPostgreSQLConnection() error {
	log.Info("connecting to postgresql")
	db, err := common.OpenDatabase(config.C.PostgreSQL.DSN)
	if err != nil {
		return errors.Wrap(err, "database connection error")
	}
	config.C.PostgreSQL.DB = db
	return nil
}

func setGatewayBackend() error {
	var err error
	var gw backend.Gateway

	switch config.C.NetworkServer.Gateway.Backend.Type {
	case "mqtt":
		gw, err = mqtt.NewBackend(
			config.C.Redis.Pool,
			config.C.NetworkServer.Gateway.Backend.MQTT,
		)
	case "gcp_pub_sub":
		gw, err = gcppubsub.NewBackend(config.C.NetworkServer.Gateway.Backend.GCPPubSub)
	default:
		return fmt.Errorf("unexpected gateway backend type: %s", config.C.NetworkServer.Gateway.Backend.Type)
	}

	if err != nil {
		return errors.Wrap(err, "gateway-backend setup failed")
	}
	config.C.NetworkServer.Gateway.Backend.Backend = gw
	return nil
}

func setApplicationServer() error {
	config.C.ApplicationServer.Pool = asclient.NewPool()
	return nil
}

func setGeolocationServer() error {
	if config.C.GeolocationServer.Server == "" {
		log.Info("no geolocation-server configured")
		return nil
	}

	log.WithFields(log.Fields{
		"server":   config.C.GeolocationServer.Server,
		"ca_cert":  config.C.GeolocationServer.CACert,
		"tls_cert": config.C.GeolocationServer.TLSCert,
		"tls_key":  config.C.GeolocationServer.TLSKey,
	}).Info("connecting to geolocation-server")

	var dialOptions []grpc.DialOption
	if config.C.GeolocationServer.TLSCert != "" && config.C.GeolocationServer.TLSKey != "" {
		dialOptions = append(dialOptions, grpc.WithTransportCredentials(
			mustGetTransportCredentials(config.C.GeolocationServer.TLSCert, config.C.GeolocationServer.TLSKey, config.C.GeolocationServer.CACert, false),
		))
	} else {
		dialOptions = append(dialOptions, grpc.WithInsecure())
	}

	geoConn, err := grpc.Dial(config.C.GeolocationServer.Server, dialOptions...)
	if err != nil {
		return errors.Wrap(err, "geolocation-server dial error")
	}
	config.C.GeolocationServer.Client = geo.NewGeolocationServerServiceClient(geoConn)

	return nil
}

func setJoinServer() error {
	var err error
	config.C.JoinServer.Pool, err = joinserver.NewPool(config.C.JoinServer)
	if err != nil {
		return errors.Wrap(err, "new join-server pool error")
	}
	return nil
}

func setNetworkController() error {
	var ncClient nc.NetworkControllerServiceClient
	if config.C.NetworkController.Server != "" {
		// setup network-controller client
		log.WithFields(log.Fields{
			"server":   config.C.NetworkController.Server,
			"ca-cert":  config.C.NetworkController.CACert,
			"tls-cert": config.C.NetworkController.TLSCert,
			"tls-key":  config.C.NetworkController.TLSKey,
		}).Info("connecting to network-controller")
		var ncDialOptions []grpc.DialOption
		if config.C.NetworkController.TLSCert != "" && config.C.NetworkController.TLSKey != "" {
			ncDialOptions = append(ncDialOptions, grpc.WithTransportCredentials(
				mustGetTransportCredentials(config.C.NetworkController.TLSCert, config.C.NetworkController.TLSKey, config.C.NetworkController.CACert, false),
			))
		} else {
			ncDialOptions = append(ncDialOptions, grpc.WithInsecure())
		}
		ncConn, err := grpc.Dial(config.C.NetworkController.Server, ncDialOptions...)
		if err != nil {
			return errors.Wrap(err, "network-controller dial error")
		}
		ncClient = nc.NewNetworkControllerServiceClient(ncConn)
	} else {
		log.Info("no network-controller configured")
		ncClient = &controller.NopNetworkControllerClient{}
	}
	config.C.NetworkController.Client = ncClient
	return nil
}

func runDatabaseMigrations() error {
	if config.C.PostgreSQL.Automigrate {
		log.Info("applying database migrations")
		m := &migrate.AssetMigrationSource{
			Asset:    migrations.Asset,
			AssetDir: migrations.AssetDir,
			Dir:      "",
		}
		n, err := migrate.Exec(config.C.PostgreSQL.DB.DB.DB, "postgres", m, migrate.Up)
		if err != nil {
			return errors.Wrap(err, "applying migrations failed")
		}
		log.WithField("count", n).Info("migrations applied")
	}
	return nil
}

func gRPCLoggingServerOptions() []grpc.ServerOption {
	logrusEntry := log.NewEntry(log.StandardLogger())
	logrusOpts := []grpc_logrus.Option{
		grpc_logrus.WithLevels(grpc_logrus.DefaultCodeToLevel),
	}

	return []grpc.ServerOption{
		grpc_middleware.WithUnaryServerChain(
			grpc_ctxtags.UnaryServerInterceptor(grpc_ctxtags.WithFieldExtractor(grpc_ctxtags.CodeGenRequestFieldExtractor)),
			grpc_logrus.UnaryServerInterceptor(logrusEntry, logrusOpts...),
		),
		grpc_middleware.WithStreamServerChain(
			grpc_ctxtags.StreamServerInterceptor(grpc_ctxtags.WithFieldExtractor(grpc_ctxtags.CodeGenRequestFieldExtractor)),
			grpc_logrus.StreamServerInterceptor(logrusEntry, logrusOpts...),
		),
	}
}

func startAPIServer() error {
	log.WithFields(log.Fields{
		"bind":     config.C.NetworkServer.API.Bind,
		"ca-cert":  config.C.NetworkServer.API.CACert,
		"tls-cert": config.C.NetworkServer.API.TLSCert,
		"tls-key":  config.C.NetworkServer.API.TLSKey,
	}).Info("starting api server")

	opts := gRPCLoggingServerOptions()
	if config.C.NetworkServer.API.CACert != "" && config.C.NetworkServer.API.TLSCert != "" && config.C.NetworkServer.API.TLSKey != "" {
		creds := mustGetTransportCredentials(config.C.NetworkServer.API.TLSCert, config.C.NetworkServer.API.TLSKey, config.C.NetworkServer.API.CACert, true)
		opts = append(opts, grpc.Creds(creds))
	}
	gs := grpc.NewServer(opts...)
	nsAPI := api.NewNetworkServerAPI()
	ns.RegisterNetworkServerServiceServer(gs, nsAPI)

	ln, err := net.Listen("tcp", config.C.NetworkServer.API.Bind)
	if err != nil {
		return errors.Wrap(err, "start api listener error")
	}
	go gs.Serve(ln)
	return nil
}

func startLoRaServer(server *uplink.Server) func() error {
	return func() error {
		*server = *uplink.NewServer()
		return server.Start()
	}
}

func startStatsServer(gwStats *gateway.StatsHandler) func() error {
	return func() error {
		*gwStats = *gateway.NewStatsHandler()
		if err := gwStats.Start(); err != nil {
			log.Fatal(err)
		}
		return nil
	}
}

func startQueueScheduler() error {
	log.Info("starting downlink device-queue scheduler")
	go downlink.DeviceQueueSchedulerLoop()

	log.Info("starting multicast scheduler")
	go downlink.MulticastQueueSchedulerLoop()

	return nil
}

func mustGetTransportCredentials(tlsCert, tlsKey, caCert string, verifyClientCert bool) credentials.TransportCredentials {
	cert, err := tls.LoadX509KeyPair(tlsCert, tlsKey)
	if err != nil {
		log.WithFields(log.Fields{
			"cert": tlsCert,
			"key":  tlsKey,
		}).Fatalf("load key-pair error: %s", err)
	}

	var caCertPool *x509.CertPool
	if caCert != "" {
		rawCaCert, err := ioutil.ReadFile(caCert)
		if err != nil {
			log.WithField("ca", caCert).Fatalf("load ca cert error: %s", err)
		}

		caCertPool = x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(rawCaCert) {
			log.WithField("ca_cert", caCert).Fatal("append ca certificate error")
		}
	}

	if verifyClientCert {
		return credentials.NewTLS(&tls.Config{
			Certificates: []tls.Certificate{cert},
			ClientCAs:    caCertPool,
			ClientAuth:   tls.RequireAndVerifyClientCert,
		})
	}

	return credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      caCertPool,
	})
}

func fixV2RedisCache() error {
	return code.Migrate(config.C.PostgreSQL.DB, "v1_to_v2_flush_profiles_cache", func() error {
		return code.FlushProfilesCache(config.C.Redis.Pool, config.C.PostgreSQL.DB)
	})
}

func migrateGatewayStats() error {
	return code.Migrate(config.C.PostgreSQL.DB, "migrate_gateway_stats_to_redis", func() error {
		return code.MigrateGatewayStats(config.C.Redis.Pool, config.C.PostgreSQL.DB)
	})
}
