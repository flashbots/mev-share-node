package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/flashbots/mev-share-node/jsonrpcserver"
	"github.com/flashbots/mev-share-node/mevshare"
	"github.com/flashbots/mev-share-node/simqueue"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/time/rate"
)

var (
	version = "dev" // is set during build process

	// Default values
	defaultDebug                 = os.Getenv("DEBUG") == "1"
	defaultLogProd               = os.Getenv("LOG_PROD") == "1"
	defaultLogService            = os.Getenv("LOG_SERVICE")
	defaultPort                  = getEnvOrDefault("PORT", "8080")
	defaultChannelName           = getEnvOrDefault("REDIS_CHANNEL_NAME", "hints")
	defaultRedisEndpoint         = getEnvOrDefault("REDIS_ENDPOINT", "redis://localhost:6379")
	defaultSimulationsEndpoint   = getEnvOrDefault("SIMULATION_ENDPOINTS", "http://127.0.0.1:8545")
	defaultWorkersPerNode        = getEnvOrDefault("WORKERS_PER_SIM_ENDPOINT", "2")
	defaultBuildersEndpoint      = getEnvOrDefault("BUILDER_ENDPOINTS", "http://127.0.0.1:8545")
	defaultPostgresDSN           = getEnvOrDefault("POSTGRES_DSN", "postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable")
	defaultEthEndpoint           = getEnvOrDefault("ETH_ENDPOINT", "http://127.0.0.1:8545")
	defaultMevSimBundleRateLimit = getEnvOrDefault("MEV_SIM_BUNDLE_RATE_LIMIT", "5")
	defaultExternalBuilders      = getEnvOrDefault("EXTERNAL_BUILDERS", "")
	defaultShareGasUsed          = getEnvOrDefault("SHARE_GAS_USED", "0")
	defaultShareMevGasPrice      = getEnvOrDefault("SHARE_MEV_GAS_PRICE", "1")

	// Flags
	debugPtr                 = flag.Bool("debug", defaultDebug, "print debug output")
	logProdPtr               = flag.Bool("log-prod", defaultLogProd, "log in production mode (json)")
	logServicePtr            = flag.String("log-service", defaultLogService, "'service' tag to logs")
	portPtr                  = flag.String("port", defaultPort, "port to listen on")
	channelPtr               = flag.String("channel", defaultChannelName, "redis pub/sub channel name string")
	redisPtr                 = flag.String("redis", defaultRedisEndpoint, "redis url string")
	simEndpointPtr           = flag.String("sim-endpoint", defaultSimulationsEndpoint, "simulation endpoints (comma separated)")
	workersPerNodePtr        = flag.String("workers-per-node", defaultWorkersPerNode, "number of workers per simulation node")
	buildersEndpointPtr      = flag.String("builder-endpoint", defaultBuildersEndpoint, "builder endpoint")
	postgresDSNPtr           = flag.String("postgres-dsn", defaultPostgresDSN, "postgres dsn")
	ethPtr                   = flag.String("eth", defaultEthEndpoint, "eth endpoint")
	meVSimBundleRateLimitPtr = flag.String("mev-sim-bundle-rate-limit", defaultMevSimBundleRateLimit, "mev sim bundle rate limit for external users (calls per second)")
	externalBuildersPtr      = flag.String("external-builders", defaultExternalBuilders, "external builders (e.g. name,rpc1,api;name,rpc2,api)")
	shareGasUsedPtr          = flag.String("share-gas-used", defaultShareGasUsed, "share gas used in hints (0-1)")
	shareMevGasPricePtr      = flag.String("share-mev-gas-price", defaultShareMevGasPrice, "share mev gas price in hints (0-1)")
)

func main() {
	flag.Parse()

	logger, _ := zap.NewDevelopment()
	if *logProdPtr {
		atom := zap.NewAtomicLevel()
		if *debugPtr {
			atom.SetLevel(zap.DebugLevel)
		}

		encoderCfg := zap.NewProductionEncoderConfig()
		encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder
		logger = zap.New(zapcore.NewCore(
			zapcore.NewJSONEncoder(encoderCfg),
			zapcore.Lock(os.Stdout),
			atom,
		))
	}
	defer func() { _ = logger.Sync() }()
	if *logServicePtr != "" {
		logger = logger.With(zap.String("service", *logServicePtr))
	}

	ctx, ctxCancel := context.WithCancel(context.Background())

	logger.Info("Starting mev-share-node", zap.String("version", version))

	redisOpts, err := redis.ParseURL(*redisPtr)
	if err != nil {
		logger.Fatal("Failed to parse redis url", zap.Error(err))
	}
	redisClient := redis.NewClient(redisOpts)

	var simBackends []mevshare.SimulationBackend //nolint:prealloc
	for _, simEndpoint := range strings.Split(*simEndpointPtr, ",") {
		simBackend := mevshare.NewJSONRPCSimulationBackend(simEndpoint)
		simBackends = append(simBackends, simBackend)
	}

	hintBackend := mevshare.NewRedisHintBackend(redisClient, *channelPtr)
	if err != nil {
		logger.Fatal("Failed to create redis hint backend", zap.Error(err))
	}

	var builderBackends []mevshare.BuilderBackend //nolint:prealloc
	for _, builderEndpoint := range strings.Split(*buildersEndpointPtr, ",") {
		builderBackend := mevshare.NewJSONRPCBuilder(builderEndpoint)
		builderBackends = append(builderBackends, builderBackend)
	}

	ethBackend, err := ethclient.Dial(*ethPtr)
	if err != nil {
		logger.Fatal("Failed to connect to ethBackend endpoint", zap.Error(err))
	}

	dbBackend, err := mevshare.NewDBBackend(*postgresDSNPtr)
	if err != nil {
		logger.Fatal("Failed to create postgres backend", zap.Error(err))
	}

	externalBuilders, err := mevshare.ParseExternalBuilders(*externalBuildersPtr)
	if err != nil {
		logger.Fatal("Failed to parse external builders", zap.Error(err))
	}

	shareGasUsed := *shareGasUsedPtr == "1"
	shareMevGasPrice := *shareMevGasPricePtr == "1"
	simResultBackend := mevshare.NewSimulationResultBackend(logger, hintBackend, builderBackends, ethBackend, dbBackend, externalBuilders, shareGasUsed, shareMevGasPrice)

	redisQueue := simqueue.NewRedisQueue(logger, redisClient, "node")

	var workersPerNode int
	if _, err := fmt.Sscanf(*workersPerNodePtr, "%d", &workersPerNode); err != nil {
		logger.Fatal("Failed to parse workers per node", zap.Error(err))
	}
	if workersPerNode < 1 {
		logger.Fatal("Workers per node must be greater than 0")
	}
	backgroundWg := &sync.WaitGroup{}
	simQueue := mevshare.NewQueue(logger, redisQueue, ethBackend, simBackends, simResultBackend, workersPerNode, backgroundWg)
	queueWg := simQueue.Start(ctx)
	// chain id
	chainID, err := ethBackend.ChainID(ctx)
	if err != nil {
		logger.Fatal("Failed to get chain id", zap.Error(err))
	}
	signer := types.LatestSignerForChainID(chainID)

	rateLimit, err := strconv.ParseFloat(*meVSimBundleRateLimitPtr, 64)
	if err != nil {
		logger.Fatal("Failed to parse mev sim bundle rate limit", zap.Error(err))
	}

	cachingEthBackend := mevshare.NewCachingEthClient(ethBackend)

	api := mevshare.NewAPI(logger, simQueue, dbBackend, cachingEthBackend, signer, simBackends, rate.Limit(rateLimit), builderBackends)

	jsonRPCServer, err := jsonrpcserver.NewHandler(jsonrpcserver.Methods{
		"mev_sendBundle":         api.SendBundle,
		"mev_simBundle":          api.SimBundle,
		"mev_cancelBundleByHash": api.CancelBundleByHash,
	})
	if err != nil {
		logger.Fatal("Failed to create jsonrpc server", zap.Error(err))
	}

	http.Handle("/", jsonRPCServer)
	server := &http.Server{
		Addr:              fmt.Sprintf(":%s", *portPtr),
		ReadHeaderTimeout: 5 * time.Second,
	}

	connectionsClosed := make(chan struct{})
	go func() {
		notifier := make(chan os.Signal, 1)
		signal.Notify(notifier, os.Interrupt, syscall.SIGTERM)
		<-notifier
		logger.Info("Shutting down...")
		ctxCancel()
		if err := server.Shutdown(context.Background()); err != nil {
			logger.Error("Failed to shutdown server", zap.Error(err))
		}
		close(connectionsClosed)
	}()

	err = server.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Fatal("ListenAndServe: ", zap.Error(err))
	}

	<-ctx.Done()
	<-connectionsClosed
	// wait for queue to finish processing
	queueWg.Wait()
	backgroundWg.Wait()
}

func getEnvOrDefault(key, defaultValue string) string {
	ret := os.Getenv(key)
	if ret == "" {
		ret = defaultValue
	}
	return ret
}
