package main

import (
	"fmt"
	"os"

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

func loadConfig() (client.ClientConfig, error) {
	agencyId, err := requiredEnv("AGENCY_ID")
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

	return client.ClientConfig{
		ServerHost: serverHost,
		ServerPort: serverPort,
		AgencyId:   agencyId,
		InputFile:  inputFile,
		OutputFile: outputFile,
	}, nil
}

func run() int {
	config, err := loadConfig()
	if err != nil {
		logger.Error("load-config", logger.Fail, "err", err)
		return 1
	}

	client, err := client.NewClient(config)
	if err != nil {
		logger.Error("client-new", logger.Fail, "err", err)
		return 1
	}

	if err := client.Run(); err != nil {
		logger.Error("client-run", logger.Fail, "err", err)
		return 1
	}
	return 0
}

func main() {
	os.Exit(run())
}
