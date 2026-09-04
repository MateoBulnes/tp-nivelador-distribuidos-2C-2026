package client

import (
	"net"
	"time"

	"github.com/7574-sistemas-distribuidos/tp-nivelador/src/bets"
	"github.com/7574-sistemas-distribuidos/tp-nivelador/src/logger"
	"github.com/7574-sistemas-distribuidos/tp-nivelador/src/protocol"
)

const CONNECTION_ATTEMPTS_MAX = 15
const CONNECTION_ATTEMPS_DELAY_MS = 200

type ClientConfig struct {
	ServerHost string
	ServerPort string
	AgencyId   uint16
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

func (client *Client) Run() error {
	defer client.conn.Close()

	proto := protocol.New(client.conn)

	if err := proto.SendHello(client.config.AgencyId); err != nil {
		logger.Error("send-hello", logger.Fail, "agency-id", client.config.AgencyId, "err", err)
		return err
	}

	if err := client.sendBets(proto); err != nil {
		return err
	}

	winners, err := client.recvWinners(proto)
	if err != nil {
		return err
	}

	return client.storeWinners(winners)
}

func (client *Client) sendBets(proto *protocol.Protocol) error {
	const action = "send-bets"
	agencyArgs := []any{"agency-id", client.config.AgencyId}

	reader, err := bets.NewReader(client.config.InputFile)
	if err != nil {
		logger.Error(action, logger.Fail, append(agencyArgs, "err", err)...)
		return err
	}
	defer reader.Close()

	logger.Info(action, logger.InProgress, agencyArgs...)

	betsAmount := 0
	for reader.Next() {
		if err := exchangeBet(proto, reader); err != nil {
			logger.Error(action, logger.Fail, append(agencyArgs, "bets-amount", betsAmount, "err", err)...)
			return err
		}

		betsAmount++
	}

	if err := reader.Err(); err != nil {
		logger.Error(action, logger.Fail, append(agencyArgs, "bets-amount", betsAmount, "err", err)...)
		return err
	}

	logger.Info(action, logger.Success, append(agencyArgs, "bets-amount", betsAmount)...)
	return nil
}

func exchangeBet(proto *protocol.Protocol, reader *bets.Reader) error {
	bet, err := reader.Bet()
	if err != nil {
		return err
	}

	if err := proto.SendBet(bet); err != nil {
		return err
	}

	return proto.RecvAck()
}

func (client *Client) recvWinners(proto *protocol.Protocol) ([]bets.Bet, error) {
	const action = "recv-winners"
	agencyArgs := []any{"agency-id", client.config.AgencyId}

	logger.Info(action, logger.InProgress, agencyArgs...)

	if err := proto.SendFinished(); err != nil {
		logger.Error(action, logger.Fail, append(agencyArgs, "err", err)...)
		return nil, err
	}

	winners, err := proto.RecvWinners()
	if err != nil {
		logger.Error(action, logger.Fail, append(agencyArgs, "err", err)...)
		return nil, err
	}

	logger.Info(action, logger.Success, append(agencyArgs, "winners-amount", len(winners))...)
	return winners, nil
}

func (client *Client) storeWinners(winners []bets.Bet) (err error) {
	const action = "store-winners"

	writer, err := bets.NewWriter(client.config.OutputFile)
	if err != nil {
		logger.Error(action, logger.Fail, "err", err)
		return err
	}

	defer func() {
		if closeErr := writer.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	for _, winner := range winners {
		if err := writer.WriteBet(winner); err != nil {
			logger.Error(action, logger.Fail, "err", err)
			return err
		}
	}

	logger.Info(action, logger.Success, "winners-amount", len(winners))
	return nil
}
