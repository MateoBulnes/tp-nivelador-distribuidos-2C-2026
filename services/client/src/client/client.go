package client

import (
	"errors"
	"net"
	"time"

	"github.com/7574-sistemas-distribuidos/tp-nivelador/src/bets"
	"github.com/7574-sistemas-distribuidos/tp-nivelador/src/logger"
	"github.com/7574-sistemas-distribuidos/tp-nivelador/src/protocol"
)

const CONNECTION_ATTEMPTS_MAX = 15
const CONNECTION_ATTEMPS_DELAY_MS = 200

var ErrShutdown = errors.New("shutdown requested")

type ClientConfig struct {
	ServerHost string
	ServerPort string
	AgencyId   uint16
	InputFile  string
	OutputFile string
	BatchSize  int
}

type Client struct {
	conn     net.Conn
	config   ClientConfig
	shutdown <-chan struct{}
}

func NewClient(config ClientConfig, shutdown <-chan struct{}) (*Client, error) {
	conn, err := connectToServer(config.ServerHost, config.ServerPort, shutdown)
	if err != nil {
		if !errors.Is(err, ErrShutdown) {
			logger.Warn("connect-to-server", logger.Fail)
		}
		return nil, err
	}

	client := &Client{conn: conn, config: config, shutdown: shutdown}
	return client, nil
}

func connectToServer(host, port string, shutdown <-chan struct{}) (net.Conn, error) {
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
			select {
			case <-time.After(CONNECTION_ATTEMPS_DELAY_MS * time.Millisecond):
			case <-shutdown:
				return nil, ErrShutdown
			}
		}
	}

	return conn, err
}

func (client *Client) Run() error {
	defer client.conn.Close()

	done := make(chan struct{})
	defer close(done)
	go client.watchShutdown(done)

	err := client.exchange()
	if err != nil && client.shutdownRequested() {
		return ErrShutdown
	}

	return err
}

func (client *Client) exchange() error {
	proto := protocol.New(client.conn, client.config.BatchSize)

	if err := proto.SendHello(client.config.AgencyId); err != nil {
		client.logFailure("send-hello", err, "agency-id", client.config.AgencyId)
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

func (client *Client) watchShutdown(done <-chan struct{}) {
	select {
	case <-client.shutdown:
		if err := client.conn.SetDeadline(time.Now()); err != nil {
			logger.Error("shutdown-connection", logger.Fail, "err", err)
		}
	case <-done:
	}
}

func (client *Client) shutdownRequested() bool {
	select {
	case <-client.shutdown:
		return true
	default:
		return false
	}
}

func (client *Client) logFailure(action string, err error, args ...any) {
	if client.shutdownRequested() {
		return
	}

	logger.Error(action, logger.Fail, append(args, "err", err)...)
}

func (client *Client) sendBets(proto *protocol.Protocol) error {
	const action = "send-bets"
	agencyArgs := []any{"agency-id", client.config.AgencyId}

	reader, err := bets.NewReader(client.config.InputFile)
	if err != nil {
		client.logFailure(action, err, agencyArgs...)
		return err
	}
	defer reader.Close()

	logger.Info(action, logger.InProgress, agencyArgs...)

	betsAmount, batchesAmount, err := exchangeBets(proto, reader)
	amountArgs := append(agencyArgs, "bets-amount", betsAmount, "batches-amount", batchesAmount)
	if err != nil {
		client.logFailure(action, err, amountArgs...)
		return err
	}

	logger.Info(action, logger.Success, amountArgs...)
	return nil
}

func exchangeBets(proto *protocol.Protocol, reader *bets.Reader) (int, int, error) {
	betsAmount := 0
	batchesAmount := 0

	sendBatch := func() error {
		if err := proto.SendBatch(); err != nil {
			return err
		}

		if err := proto.RecvAck(); err != nil {
			return err
		}

		batchesAmount++
		proto.BeginBatch()
		return nil
	}

	proto.BeginBatch()
	for reader.Next() {
		bet, err := reader.Bet()
		if err != nil {
			return betsAmount, batchesAmount, err
		}

		added, err := proto.AddBet(bet)
		if err != nil {
			return betsAmount, batchesAmount, err
		}

		if !added {
			if err := sendBatch(); err != nil {
				return betsAmount, batchesAmount, err
			}

			if _, err := proto.AddBet(bet); err != nil {
				return betsAmount, batchesAmount, err
			}
		}

		betsAmount++
	}

	if err := reader.Err(); err != nil {
		return betsAmount, batchesAmount, err
	}

	if !proto.BatchIsEmpty() {
		if err := sendBatch(); err != nil {
			return betsAmount, batchesAmount, err
		}
	}

	return betsAmount, batchesAmount, nil
}

func (client *Client) recvWinners(proto *protocol.Protocol) ([]bets.Bet, error) {
	const action = "recv-winners"
	agencyArgs := []any{"agency-id", client.config.AgencyId}

	logger.Info(action, logger.InProgress, agencyArgs...)

	if err := proto.SendFinished(); err != nil {
		client.logFailure(action, err, agencyArgs...)
		return nil, err
	}

	winners, err := proto.RecvWinners()
	if err != nil {
		client.logFailure(action, err, agencyArgs...)
		return nil, err
	}

	logger.Info(action, logger.Success, append(agencyArgs, "winners-amount", len(winners))...)
	return winners, nil
}

func (client *Client) storeWinners(winners []bets.Bet) (err error) {
	const action = "store-winners"

	writer, err := bets.NewWriter(client.config.OutputFile)
	if err != nil {
		client.logFailure(action, err)
		return err
	}

	defer func() {
		if closeErr := writer.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	for _, winner := range winners {
		if err := writer.WriteBet(winner); err != nil {
			client.logFailure(action, err)
			return err
		}
	}

	logger.Info(action, logger.Success, "winners-amount", len(winners))
	return nil
}
