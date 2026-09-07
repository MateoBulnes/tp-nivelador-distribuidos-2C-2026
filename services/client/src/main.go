package main

import (
	"errors"
	"fmt"
	"math"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	client "github.com/7574-sistemas-distribuidos/tp-nivelador/src/client"
	"github.com/7574-sistemas-distribuidos/tp-nivelador/src/logger"
)

func requiredEnv(key string) (string, error) {
	value := os.Getenv(key)
	if value == "" {
		return "", fmt.Errorf("%s environment variable is required", key)
	}

	return value, nil
}

func requiredEnvUint16(key string) (uint16, error) {
	value, err := requiredEnv(key)
	if err != nil {
		return 0, err
	}

	parsed, err := strconv.ParseUint(value, 10, 16)
	if err != nil {
		return 0, fmt.Errorf(
			"%s must be an integer between 0 and %d: %w",
			key, math.MaxUint16, err,
		)
	}

	return uint16(parsed), nil
}

func requiredEnvPositiveInt(key string) (int, error) {
	value, err := requiredEnv(key)
	if err != nil {
		return 0, err
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", key, err)
	}

	if parsed < 1 {
		return 0, fmt.Errorf("%s must be greater than zero, got %d", key, parsed)
	}

	return parsed, nil
}

func loadConfig() (client.ClientConfig, error) {
	agencyId, err := requiredEnvUint16("AGENCY_ID")
	if err != nil {
		return client.ClientConfig{}, err
	}

	serverHost, err := requiredEnv("SERVER_HOST")
	if err != nil {
		return client.ClientConfig{}, err
	}

	serverPort, err := requiredEnv("SERVER_PORT")
	if err != nil {
		return client.ClientConfig{}, err
	}

	inputFile, err := requiredEnv("INPUT_FILE")
	if err != nil {
		return client.ClientConfig{}, err
	}

	outputFile, err := requiredEnv("OUTPUT_FILE")
	if err != nil {
		return client.ClientConfig{}, err
	}

	batchSize, err := requiredEnvPositiveInt("BATCH_SIZE")
	if err != nil {
		return client.ClientConfig{}, err
	}

	return client.ClientConfig{
		ServerHost: serverHost,
		ServerPort: serverPort,
		AgencyId:   agencyId,
		InputFile:  inputFile,
		OutputFile: outputFile,
		BatchSize:  batchSize,
	}, nil
}

func notifyShutdown() <-chan struct{} {
	shutdown := make(chan struct{})
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM)

	go func() {
		<-signals
		logger.Info("sigterm-received", logger.InProgress)
		close(shutdown)
	}()

	return shutdown
}

func reportFailure(stage string, err error) int {
	if errors.Is(err, client.ErrShutdown) {
		logger.Info("graceful-shutdown", logger.Success, "stage", stage)
		return 0
	}

	logger.Error(stage, logger.Fail, "err", err)
	return 1
}

func run() int {
	shutdown := notifyShutdown()

	config, err := loadConfig()
	if err != nil {
		logger.Error("load-config", logger.Fail, "err", err)
		return 1
	}

	agency, err := client.NewClient(config, shutdown)
	if err != nil {
		return reportFailure("client-new", err)
	}

	if err := agency.Run(); err != nil {
		return reportFailure("client-run", err)
	}
	return 0
}

func main() {
	os.Exit(run())
}
