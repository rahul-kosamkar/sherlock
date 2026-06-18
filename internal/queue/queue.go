package queue

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

type Queue struct {
	conn   *nats.Conn
	js     jetstream.JetStream
	stream jetstream.Stream
}

func New(ctx context.Context, natsURL string, streamName string) (*Queue, error) {
	nc, err := nats.Connect(natsURL)
	if err != nil {
		return nil, fmt.Errorf("connecting to NATS: %w", err)
	}

	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("creating JetStream context: %w", err)
	}

	stream, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      streamName,
		Subjects:  []string{streamName + ".*"},
		Retention: jetstream.WorkQueuePolicy,
	})
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("creating stream %q: %w", streamName, err)
	}

	return &Queue{
		conn:   nc,
		js:     js,
		stream: stream,
	}, nil
}

func (q *Queue) Publish(ctx context.Context, subject string, data []byte) error {
	_, err := q.js.Publish(ctx, subject, data)
	if err != nil {
		return fmt.Errorf("publishing to %q: %w", subject, err)
	}
	return nil
}

func (q *Queue) Subscribe(ctx context.Context, subject string, handler func(msg []byte, ack func(), nak func()) error) error {
	consumer, err := q.stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:       sanitizeDurable(subject),
		FilterSubject: subject,
		AckPolicy:     jetstream.AckExplicitPolicy,
		MaxDeliver:    3,
		AckWait:       5 * time.Minute,
		MaxAckPending: 10,
	})
	if err != nil {
		return fmt.Errorf("creating consumer for %q: %w", subject, err)
	}

	cons, err := consumer.Consume(func(msg jetstream.Msg) {
		ack := func() { _ = msg.Ack() }
		nak := func() { _ = msg.Nak() }
		if err := handler(msg.Data(), ack, nak); err != nil {
			_ = msg.Nak()
		}
	})
	if err != nil {
		return fmt.Errorf("starting consumer for %q: %w", subject, err)
	}

	go func() {
		<-ctx.Done()
		cons.Stop()
	}()

	return nil
}

func (q *Queue) Close() {
	_ = q.conn.Drain()
	q.conn.Close()
}

// sanitizeDurable converts a subject like "INVESTIGATIONS.new" into a safe
// durable consumer name by replacing dots with dashes.
func sanitizeDurable(subject string) string {
	out := make([]byte, len(subject))
	for i := range subject {
		if subject[i] == '.' || subject[i] == '*' || subject[i] == '>' {
			out[i] = '-'
		} else {
			out[i] = subject[i]
		}
	}
	return string(out)
}
