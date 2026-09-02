package client

import (
	"net"
	"time"

	"github.com/7574-sistemas-distribuidos/tp-nivelador/src/bets"
	"github.com/7574-sistemas-distribuidos/tp-nivelador/src/logger"
	"github.com/7574-sistemas-distribuidos/tp-nivelador/src/safe_socket"
)

const CONNECTION_ATTEMPTS_MAX = 15
const CONNECTION_ATTEMPS_DELAY_MS = 200

type ClientConfig struct {
	ServerHost string
	ServerPort string
	AgencyId   string
	InputFile  string
	OutputFile string
}

type Client struct {
	conn   net.Conn
	config ClientConfig
}

func NewClient(config ClientConfig) (*Client, error) {
	conn, err := connectToServer(config.ServerHost, config.ServerPort)
	if err != nil {
		logger.Warn("connect-to-server", logger.Fail)
		return nil, err
	}

	client := &Client{conn: conn, config: config}
	return client, nil
}

func connectToServer(host, port string) (net.Conn, error) {
	const action = "connect-to-server"
	var err error
	var conn net.Conn

	logger.Info(action, logger.InProgress)
	for attempt := range CONNECTION_ATTEMPTS_MAX {
		conn, err = net.Dial("tcp", host+":"+port)
		if err == nil {
			logger.Info(action, logger.Success)
			break
		}

		logger.Warn(action, logger.Fail, "attempt", attempt)
		if attempt < CONNECTION_ATTEMPTS_MAX-1 {
			time.Sleep(CONNECTION_ATTEMPS_DELAY_MS * time.Millisecond)
		}
	}

	return conn, err
}

func (client *Client) Run() (err error) {
	const action = "send-bets"
	agencyArgs := []any{"agency-id", client.config.AgencyId}

	defer client.conn.Close()

	reader, err := bets.NewReader(client.config.InputFile)
	if err != nil {
		logger.Error("open-input-file", logger.Fail, "err", err)
		return err
	}
	defer reader.Close()

	writer, err := bets.NewWriter(client.config.OutputFile)
	if err != nil {
		logger.Error("open-output-file", logger.Fail, "err", err)
		return err
	}
	defer func() {
		if closeErr := writer.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	logger.Info(action, logger.InProgress, agencyArgs...)

	betsAmount, err := client.exchangeBets(reader, writer)
	if err != nil {
		logger.Error(action, logger.Fail, append(agencyArgs, "bets-amount", betsAmount, "err", err)...)
		return err
	}

	logger.Info(action, logger.Success, append(agencyArgs, "bets-amount", betsAmount)...)
	return nil
}

func (client *Client) exchangeBets(reader *bets.Reader, writer *bets.Writer) (int, error) {
	betsAmount := 0

	for reader.Next() {
		record := reader.Record()

		if err := safe_socket.SendAll(client.conn, record); err != nil {
			return betsAmount, err
		}

		response, err := safe_socket.RecvAll(client.conn, len(record))
		if err != nil {
			return betsAmount, err
		}

		if err := writer.WriteRecord(response); err != nil {
			return betsAmount, err
		}

		betsAmount++
	}

	return betsAmount, reader.Err()
}
