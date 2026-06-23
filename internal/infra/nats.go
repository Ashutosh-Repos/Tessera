package infra

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/distributed-transcoder/internal/config"
	"github.com/nats-io/nats.go"
)

type NATSBus struct {
	nc   *nats.Conn
	js   nats.JetStreamContext
	mu   sync.Mutex
	subs map[int]*nats.Subscription // cached pull subscriptions per shard
}

// NewNATSBus creates a NATS connection with mTLS client certificates.
// Per DTDP §9.2, each tier uses its own certificate for publish/subscribe scoping.
func NewNATSBus(cfg config.NATSConfig) (*NATSBus, error) {
	var tlsConfig *tls.Config

	if cfg.TLSCert != "" && cfg.TLSKey != "" && cfg.TLSCA != "" {
		// Load client certificate and key
		cert, err := tls.LoadX509KeyPair(cfg.TLSCert, cfg.TLSKey)
		if err != nil {
			return nil, fmt.Errorf("failed to load NATS client cert: %w", err)
		}

		// Load CA certificate
		caCert, err := os.ReadFile(cfg.TLSCA)
		if err != nil {
			return nil, fmt.Errorf("failed to load NATS CA cert: %w", err)
		}
		caCertPool := x509.NewCertPool()
		caCertPool.AppendCertsFromPEM(caCert)

		tlsConfig = &tls.Config{
			Certificates: []tls.Certificate{cert},
			RootCAs:      caCertPool,
			MinVersion:   tls.VersionTLS13,
		}
	}

	opts := []nats.Option{
		nats.MaxReconnects(-1), // infinite reconnects
		nats.ReconnectWait(time.Second),
		nats.ErrorHandler(func(_ *nats.Conn, _ *nats.Subscription, err error) {
			log.Printf("NATS error: %v", err)
		}),
	}

	if tlsConfig != nil {
		opts = append(opts, nats.Secure(tlsConfig))
	}

	nc, err := nats.Connect(strings.Join(cfg.URLs, ","), opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS: %w", err)
	}

	js, err := nc.JetStream()
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("failed to get JetStream context: %w", err)
	}

	return &NATSBus{nc: nc, js: js, subs: make(map[int]*nats.Subscription)}, nil
}

func (n *NATSBus) PublishTaskAsync(ctx context.Context, shard int, priority string, payload []byte) error {
	subject := fmt.Sprintf("transcode-tasks.shard.%d.%s", shard, priority)
	_, err := n.js.PublishAsync(subject, payload)
	return err
}

func (n *NATSBus) FlushPendingPublishes(ctx context.Context) error {
	select {
	case <-n.js.PublishAsyncComplete():
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (n *NATSBus) PublishEvent(ctx context.Context, subject string, payload []byte) error {
	_, err := n.js.Publish(subject, payload)
	return err
}

func (n *NATSBus) PullTasks(ctx context.Context, shard int, batchSize int) ([]TaskMessage, error) {
	// Reuse cached subscription to avoid creating a new one on every poll cycle.
	n.mu.Lock()
	sub, ok := n.subs[shard]
	if !ok {
		stream := "transcode-tasks"
		durable := fmt.Sprintf("worker-group-shard-%d", shard)
		var err error
		sub, err = n.js.PullSubscribe(fmt.Sprintf("%s.shard.%d.>", stream, shard), durable)
		if err != nil {
			n.mu.Unlock()
			return nil, err
		}
		n.subs[shard] = sub
	}
	n.mu.Unlock()

	msgs, err := sub.Fetch(batchSize, nats.Context(ctx))
	if err != nil {
		return nil, err
	}

	var tasks []TaskMessage
	for _, msg := range msgs {
		tasks = append(tasks, &NATSTaskMessage{msg: msg})
	}
	return tasks, nil
}

func (n *NATSBus) SubscribePartitionUploads(ctx context.Context, partitionID int, handler func(msg TaskMessage)) error {
	subject := fmt.Sprintf("s3-raw-uploads.job.partition_%d.>", partitionID)
	sub, err := n.js.Subscribe(subject, func(m *nats.Msg) {
		handler(&NATSTaskMessage{msg: m})
	}, nats.Durable(fmt.Sprintf("coord-upload-sub-%d", partitionID)))
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		sub.Unsubscribe()
	}()
	return nil
}

func (n *NATSBus) SubscribeCompletionEvents(ctx context.Context, partitionID int, handler func(msg TaskMessage)) error {
	subject := fmt.Sprintf("s3-transcoded.job.partition_%d.>", partitionID)
	sub, err := n.js.Subscribe(subject, func(m *nats.Msg) {
		handler(&NATSTaskMessage{msg: m})
	}, nats.Durable(fmt.Sprintf("coord-completion-sub-%d", partitionID)))
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		sub.Unsubscribe()
	}()
	return nil
}

func (n *NATSBus) SubscribeDLQ(ctx context.Context, handler func(msg TaskMessage)) error {
	sub, err := n.js.QueueSubscribe("transcode-tasks-dlq", "coord-dlq-group", func(m *nats.Msg) {
		handler(&NATSTaskMessage{msg: m})
	}, nats.Durable("coord-dlq-monitor"))
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		sub.Unsubscribe()
	}()
	return nil
}

func (n *NATSBus) Ping(ctx context.Context) error {
	if n.nc.IsClosed() {
		return fmt.Errorf("nats connection is closed")
	}
	return nil
}

// NATSTaskMessage wraps a NATS message to implement TaskMessage
type NATSTaskMessage struct {
	msg *nats.Msg
}

func (m *NATSTaskMessage) Data() []byte {
	return m.msg.Data
}

func (m *NATSTaskMessage) Ack() error {
	return m.msg.Ack()
}

func (m *NATSTaskMessage) Nak() error {
	return m.msg.Nak()
}

func (m *NATSTaskMessage) InProgress() error {
	return m.msg.InProgress()
}

func (m *NATSTaskMessage) Metadata() TaskMessageMeta {
	meta, err := m.msg.Metadata()
	if err != nil {
		return TaskMessageMeta{}
	}
	return TaskMessageMeta{
		NumDelivered: int(meta.NumDelivered),
		Timestamp:    meta.Timestamp.Unix(),
	}
}

func (n *NATSBus) InitEcosystem(shardCount int) error {
	// Create transcode-tasks stream
	_, _ = n.js.AddStream(&nats.StreamConfig{
		Name:     "transcode-tasks",
		Subjects: []string{"transcode-tasks.shard.>"},
		Storage:  nats.FileStorage,
	})

	// Create transcode-tasks-dlq stream
	_, _ = n.js.AddStream(&nats.StreamConfig{
		Name:     "transcode-tasks-dlq",
		Subjects: []string{"transcode-tasks-dlq"},
		Storage:  nats.FileStorage,
	})

	// Create job-uploads stream
	_, _ = n.js.AddStream(&nats.StreamConfig{
		Name:     "job-uploads",
		Subjects: []string{"s3-raw-uploads.job.>", "s3-transcoded.job.>"},
		Storage:  nats.FileStorage,
	})

	// Add durable pull consumers for worker shards
	for shard := 0; shard < shardCount; shard++ {
		durable := fmt.Sprintf("worker-group-shard-%d", shard)
		_, _ = n.js.AddConsumer("transcode-tasks", &nats.ConsumerConfig{
			Durable:       durable,
			DeliverPolicy: nats.DeliverAllPolicy,
			AckPolicy:     nats.AckExplicitPolicy,
			AckWait:       30 * time.Second,
			MaxDeliver:    3,
			FilterSubject: fmt.Sprintf("transcode-tasks.shard.%d.>", shard),
		})
	}

	return nil
}

// Close drains pending publishes and closes the NATS connection gracefully.
func (n *NATSBus) Close() error {
	// Drain ensures all pending async publishes complete before disconnecting
	if err := n.nc.Drain(); err != nil {
		n.nc.Close()
		return err
	}
	return nil
}
