package infra

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/distributed-transcoder/internal/config"
)

// SQSBus implements the MessageBus interface using AWS SQS.
type SQSBus struct {
	client       *sqs.Client
	cfg          config.ObjectStoreConfig // Reuses AWS credentials configuration
	mu           sync.RWMutex
	queueURLs    map[string]string // maps subject/queue name to SQS Queue URL
	subscriptions []chan TaskMessage
}

// NewSQSBus instantiates a new AWS SQS MessageBus client.
func NewSQSBus(storeCfg config.ObjectStoreConfig) (*SQSBus, error) {
	customResolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
		if storeCfg.Endpoint != "" {
			return aws.Endpoint{
				PartitionID:   "aws",
				URL:           fmt.Sprintf("http://%s", storeCfg.Endpoint), // support local SQS simulators (e.g. LocalStack)
				SigningRegion: storeCfg.Region,
			}, nil
		}
		return aws.Endpoint{}, &aws.EndpointNotFoundError{}
	})

	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion(storeCfg.Region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(storeCfg.AccessKey, storeCfg.SecretKey, "")),
		awsconfig.WithEndpointResolverWithOptions(customResolver),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load SQS AWS config: %w", err)
	}

	client := sqs.NewFromConfig(awsCfg)

	return &SQSBus{
		client:    client,
		cfg:       storeCfg,
		queueURLs: make(map[string]string),
	}, nil
}

// InitEcosystem creates/resolves the required task queue endpoints.
func (s *SQSBus) InitEcosystem(shardCount int) error {
	ctx := context.Background()

	// 1. Resolve or create task queues for each shard
	for i := 0; i < shardCount; i++ {
		queueName := fmt.Sprintf("transcode-task-shard-%d", i)
		url, err := s.resolveOrCreateQueue(ctx, queueName)
		if err != nil {
			return fmt.Errorf("failed to initialize queue %s: %w", queueName, err)
		}
		s.mu.Lock()
		s.queueURLs[queueName] = url
		s.mu.Unlock()
	}

	// 2. Resolve or create event topic queues
	eventQueues := []string{
		"transcoder-upload-events",
		"transcoder-completion-events",
		"transcoder-dlq",
	}
	for _, qName := range eventQueues {
		url, err := s.resolveOrCreateQueue(ctx, qName)
		if err != nil {
			return fmt.Errorf("failed to initialize queue %s: %w", qName, err)
		}
		s.mu.Lock()
		s.queueURLs[qName] = url
		s.mu.Unlock()
	}

	return nil
}

// PublishTaskAsync pushes a task payload into the shard-specific SQS queue.
func (s *SQSBus) PublishTaskAsync(ctx context.Context, shard int, priority string, payload []byte) error {
	queueName := fmt.Sprintf("transcode-task-shard-%d", shard)
	url, err := s.getQueueURL(ctx, queueName)
	if err != nil {
		return err
	}

	_, err = s.client.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    aws.String(url),
		MessageBody: aws.String(string(payload)),
	})
	return err
}

// FlushPendingPublishes is a no-op as SQS pushes are synchronous.
func (s *SQSBus) FlushPendingPublishes(ctx context.Context) error {
	return nil
}

// PublishEvent routes simulated storage notifications or transcoded completion updates to their aggregate queues.
func (s *SQSBus) PublishEvent(ctx context.Context, subject string, payload []byte) error {
	var targetQueue string
	if strings.Contains(subject, "s3-raw-uploads") {
		targetQueue = "transcoder-upload-events"
	} else if strings.Contains(subject, "s3-transcoded") {
		targetQueue = "transcoder-completion-events"
	} else {
		targetQueue = "transcoder-dlq"
	}

	url, err := s.getQueueURL(ctx, targetQueue)
	if err != nil {
		return err
	}

	_, err = s.client.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    aws.String(url),
		MessageBody: aws.String(string(payload)),
		MessageAttributes: map[string]types.MessageAttributeValue{
			"subject": {
				DataType:    aws.String("String"),
				StringValue: aws.String(subject),
			},
		},
	})
	return err
}

// PullTasks receives a batch of messages from the target SQS queue.
func (s *SQSBus) PullTasks(ctx context.Context, shard int, batchSize int) ([]TaskMessage, error) {
	queueName := fmt.Sprintf("transcode-task-shard-%d", shard)
	url, err := s.getQueueURL(ctx, queueName)
	if err != nil {
		return nil, err
	}

	res, err := s.client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
		QueueUrl:            aws.String(url),
		MaxNumberOfMessages: int32(batchSize),
		WaitTimeSeconds:     10, // long-polling enabled
		VisibilityTimeout:   60, // default visibility lock window
		MessageSystemAttributeNames: []types.MessageSystemAttributeName{
			"ApproximateReceiveCount",
		},
	})
	if err != nil {
		return nil, err
	}

	var msgs []TaskMessage
	for _, m := range res.Messages {
		var deliverCount int
		if val, ok := m.Attributes["ApproximateReceiveCount"]; ok {
			deliverCount, _ = strconv.Atoi(val)
		}
		msgs = append(msgs, &SQSTaskMessage{
			client:   s.client,
			queueURL: url,
			msg:      m,
			meta: TaskMessageMeta{
				NumDelivered: deliverCount,
				Timestamp:    time.Now().Unix(),
			},
		})
	}
	return msgs, nil
}

// SubscribePartitionUploads polls the upload events queue and filters by partition index.
func (s *SQSBus) SubscribePartitionUploads(ctx context.Context, partitionID int, handler func(msg TaskMessage)) error {
	url, err := s.getQueueURL(ctx, "transcoder-upload-events")
	if err != nil {
		return err
	}
	go s.pollEventsLoop(ctx, url, fmt.Sprintf("partition_%d", partitionID), handler)
	return nil
}

// SubscribeCompletionEvents polls the completion events queue and filters by partition index.
func (s *SQSBus) SubscribeCompletionEvents(ctx context.Context, partitionID int, handler func(msg TaskMessage)) error {
	url, err := s.getQueueURL(ctx, "transcoder-completion-events")
	if err != nil {
		return err
	}
	go s.pollEventsLoop(ctx, url, fmt.Sprintf("partition_%d", partitionID), handler)
	return nil
}

// SubscribeDLQ listens for events diverted to the Dead Letter Queue.
func (s *SQSBus) SubscribeDLQ(ctx context.Context, handler func(msg TaskMessage)) error {
	url, err := s.getQueueURL(ctx, "transcoder-dlq")
	if err != nil {
		return err
	}
	go s.pollEventsLoop(ctx, url, "", handler)
	return nil
}

// GetDLQDepth retrieves the current approximate number of visible messages in the DLQ queue.
func (s *SQSBus) GetDLQDepth() (int64, error) {
	ctx := context.Background()
	url, err := s.getQueueURL(ctx, "transcoder-dlq")
	if err != nil {
		return 0, err
	}

	res, err := s.client.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl: aws.String(url),
		AttributeNames: []types.QueueAttributeName{
			types.QueueAttributeNameApproximateNumberOfMessages,
		},
	})
	if err != nil {
		return 0, err
	}

	val := res.Attributes[string(types.QueueAttributeNameApproximateNumberOfMessages)]
	depth, _ := strconv.ParseInt(val, 10, 64)
	return depth, nil
}

// Ping checks SQS client responsiveness.
func (s *SQSBus) Ping(ctx context.Context) error {
	_, err := s.client.ListQueues(ctx, &sqs.ListQueuesInput{MaxResults: aws.Int32(1)})
	return err
}

// Helper methods

func (s *SQSBus) resolveOrCreateQueue(ctx context.Context, name string) (string, error) {
	// Try getting existing queue URL
	res, err := s.client.GetQueueUrl(ctx, &sqs.GetQueueUrlInput{
		QueueName: aws.String(name),
	})
	if err == nil {
		return *res.QueueUrl, nil
	}

	// Create queue if it does not exist
	createRes, err := s.client.CreateQueue(ctx, &sqs.CreateQueueInput{
		QueueName: aws.String(name),
		Attributes: map[string]string{
			"VisibilityTimeout": "60",
		},
	})
	if err != nil {
		return "", err
	}
	return *createRes.QueueUrl, nil
}

func (s *SQSBus) getQueueURL(ctx context.Context, name string) (string, error) {
	s.mu.RLock()
	url, exists := s.queueURLs[name]
	s.mu.RUnlock()
	if exists {
		return url, nil
	}

	url, err := s.resolveOrCreateQueue(ctx, name)
	if err != nil {
		return "", err
	}

	s.mu.Lock()
	s.queueURLs[name] = url
	s.mu.Unlock()
	return url, nil
}

func (s *SQSBus) pollEventsLoop(ctx context.Context, queueURL string, filterTag string, handler func(msg TaskMessage)) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			res, err := s.client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
				QueueUrl:            aws.String(queueURL),
				MaxNumberOfMessages: 5,
				WaitTimeSeconds:     5,
				MessageAttributeNames: []string{"subject"},
				MessageSystemAttributeNames: []types.MessageSystemAttributeName{
					"ApproximateReceiveCount",
				},
			})
			if err != nil {
				continue
			}

			for _, m := range res.Messages {
				// Apply partition filtration filterTag (e.g. partition_3) if present
				matchesFilter := true
				if filterTag != "" {
					body := ""
					if m.Body != nil {
						body = *m.Body
					}
					// Also check message attributes
					subjAttr := ""
					if attr, exists := m.MessageAttributes["subject"]; exists && attr.StringValue != nil {
						subjAttr = *attr.StringValue
					}

					if !strings.Contains(body, filterTag) && !strings.Contains(subjAttr, filterTag) {
						matchesFilter = false
					}
				}

				if matchesFilter {
					var deliverCount int
					if val, ok := m.Attributes["ApproximateReceiveCount"]; ok {
						deliverCount, _ = strconv.Atoi(val)
					}

					handler(&SQSTaskMessage{
						client:   s.client,
						queueURL: queueURL,
						msg:      m,
						meta: TaskMessageMeta{
							NumDelivered: deliverCount,
							Timestamp:    time.Now().Unix(),
						},
					})
				} else {
					// Delete/Ack event if it belongs to other partitions so it doesn't plug the queue
					// Standard pub-sub mapping design choice.
					_, _ = s.client.DeleteMessage(ctx, &sqs.DeleteMessageInput{
						QueueUrl:      aws.String(queueURL),
						ReceiptHandle: m.ReceiptHandle,
					})
				}
			}
		}
	}
}

func (s *SQSBus) Close() error {
	// SQS client does not maintain persistent stateful connections that require explicit closing.
	return nil
}

// SQSTaskMessage wraps an AWS SQS message to satisfy infra.TaskMessage.
type SQSTaskMessage struct {
	client   *sqs.Client
	queueURL string
	msg      types.Message
	meta     TaskMessageMeta
}

func (s *SQSTaskMessage) Data() []byte {
	if s.msg.Body == nil {
		return []byte{}
	}
	return []byte(*s.msg.Body)
}

func (s *SQSTaskMessage) Ack() error {
	ctx := context.Background()
	_, err := s.client.DeleteMessage(ctx, &sqs.DeleteMessageInput{
		QueueUrl:      aws.String(s.queueURL),
		ReceiptHandle: s.msg.ReceiptHandle,
	})
	return err
}

func (s *SQSTaskMessage) Nak() error {
	// Make message immediately visible again by changing visibility timeout to 0
	ctx := context.Background()
	_, err := s.client.ChangeMessageVisibility(ctx, &sqs.ChangeMessageVisibilityInput{
		QueueUrl:          aws.String(s.queueURL),
		ReceiptHandle:     s.msg.ReceiptHandle,
		VisibilityTimeout: 0,
	})
	return err
}

func (s *SQSTaskMessage) NakWithDelay(delay time.Duration) error {
	ctx := context.Background()
	_, err := s.client.ChangeMessageVisibility(ctx, &sqs.ChangeMessageVisibilityInput{
		QueueUrl:          aws.String(s.queueURL),
		ReceiptHandle:     s.msg.ReceiptHandle,
		VisibilityTimeout: int32(delay.Seconds()),
	})
	return err
}

func (s *SQSTaskMessage) InProgress() error {
	// Heartbeat visibility lock: extend visibility timeout by 60 seconds
	ctx := context.Background()
	_, err := s.client.ChangeMessageVisibility(ctx, &sqs.ChangeMessageVisibilityInput{
		QueueUrl:          aws.String(s.queueURL),
		ReceiptHandle:     s.msg.ReceiptHandle,
		VisibilityTimeout: 60,
	})
	return err
}

func (s *SQSTaskMessage) Metadata() TaskMessageMeta {
	return s.meta
}
