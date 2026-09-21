package rabbitmq

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"testing"
	"time"

	"github.com/plhhao/faultline/internal/config"
	"github.com/plhhao/faultline/internal/engine"
	"github.com/plhhao/faultline/internal/recorder"
)

func TestFrameRoundTripAndPublishMetadata(t *testing.T) {
	publish := make([]byte, 0, 32)
	publish = binary.BigEndian.AppendUint16(publish, 60)
	publish = binary.BigEndian.AppendUint16(publish, 40)
	publish = binary.BigEndian.AppendUint16(publish, 0)
	publish = append(publish, byte(len("events")))
	publish = append(publish, "events"...)
	publish = append(publish, byte(len("payment.created")))
	publish = append(publish, "payment.created"...)
	publish = append(publish, 0)
	want := frame{kind: frameMethod, channel: 7, body: publish}
	var buffer bytes.Buffer
	if err := writeFrame(&buffer, want); err != nil {
		t.Fatal(err)
	}
	got, err := readFrame(&buffer)
	if err != nil || got.kind != want.kind || got.channel != want.channel || !bytes.Equal(got.body, want.body) {
		t.Fatalf("%+v %v", got, err)
	}
	exchange, routingKey, ok := publishMetadata(got)
	if !ok || exchange != "events" || routingKey != "payment.created" {
		t.Fatal(exchange, routingKey, ok)
	}
}

func TestConfirmParsing(t *testing.T) {
	body := make([]byte, 13)
	binary.BigEndian.PutUint16(body, 60)
	binary.BigEndian.PutUint16(body[2:], 80)
	binary.BigEndian.PutUint64(body[4:], 42)
	body[12] = 1
	tag, multiple, ok := confirm(frame{kind: frameMethod, channel: 1, body: body})
	if !ok || tag != 42 || !multiple {
		t.Fatal(tag, multiple, ok)
	}
	body[3] = 120
	if _, _, ok := negativeConfirm(frame{kind: frameMethod, channel: 1, body: body}); !ok {
		t.Fatal("nack not parsed")
	}
}

func TestMalformedFrameRejected(t *testing.T) {
	if _, err := readFrame(bytes.NewReader([]byte{frameHeartbeat, 0, 0, 0, 0, 0, 0, 0})); err == nil {
		t.Fatal("frame without AMQP end accepted")
	}
}

func TestMultipleConfirmOrdering(t *testing.T) {
	s := &session{channels: map[uint16]*channelState{1: {pending: map[uint64]*publish{3: {sequence: 3}, 1: {sequence: 1}, 2: {sequence: 2}}}}}
	pending := s.take(1, 2, true)
	if len(pending) != 2 || pending[0].sequence != 1 || pending[1].sequence != 2 {
		t.Fatalf("multiple confirm ordering: %+v", pending)
	}
	if remaining := s.take(1, 3, false); len(remaining) != 1 || remaining[0].sequence != 3 {
		t.Fatalf("remaining confirm: %+v", remaining)
	}
}

func TestContentAndReturnMetadata(t *testing.T) {
	header := make([]byte, 12)
	binary.BigEndian.PutUint16(header, 60)
	size := uint64(42)
	binary.BigEndian.PutUint64(header[4:], size)
	if got, ok := contentSize(frame{kind: frameHeader, body: header}); !ok || got != size {
		t.Fatalf("content size: %d %t", got, ok)
	}
	returned := make([]byte, 0, 32)
	returned = binary.BigEndian.AppendUint16(returned, 60)
	returned = binary.BigEndian.AppendUint16(returned, 50)
	returned = binary.BigEndian.AppendUint16(returned, 312)
	returned = append(returned, byte(len("NO_ROUTE")))
	returned = append(returned, "NO_ROUTE"...)
	returned = append(returned, byte(len("events")))
	returned = append(returned, "events"...)
	returned = append(returned, byte(len("payment.created")))
	returned = append(returned, "payment.created"...)
	exchange, routingKey, ok := returnMetadata(frame{kind: frameMethod, body: returned})
	if !ok || exchange != "events" || routingKey != "payment.created" {
		t.Fatalf("return metadata: %q %q %t", exchange, routingKey, ok)
	}
}

func TestCloseChannelResetsState(t *testing.T) {
	records := recorder.New(io.Discard, 8)
	t.Cleanup(func() { _ = records.Close(context.Background()) })
	s := &session{server: &Server{records: records}, channels: map[uint16]*channelState{1: {confirming: true, requested: true, next: 4, pending: map[uint64]*publish{}}}}
	s.closeChannel(1)
	if _, exists := s.channels[1]; exists {
		t.Fatal("closed channel state retained")
	}
	state := s.state(1)
	if state.confirming || state.requested || state.next != 0 || len(state.pending) != 0 {
		t.Fatalf("reopened state: %+v", state)
	}
}

func TestPublishCandidateWaitsForCompleteContent(t *testing.T) {
	s := &session{channels: map[uint16]*channelState{1: {pending: map[uint64]*publish{}, candidate: &publishCandidate{exchange: "events", routingKey: "payment.created"}}}}
	header := make([]byte, 12)
	binary.BigEndian.PutUint16(header, 60)
	binary.BigEndian.PutUint64(header[4:], 2)
	if err := s.publishHeader(frame{kind: frameHeader, channel: 1, body: header}); err != nil {
		t.Fatal(err)
	}
	if err := s.publishBody(frame{kind: frameBody, channel: 1, body: []byte("x")}); err != nil {
		t.Fatal(err)
	}
	state := s.state(1)
	if state.candidate == nil || state.candidate.remaining != 1 || len(state.pending) != 0 {
		t.Fatalf("partial content created a flow: %+v", state)
	}
}

func TestMultipleConfirmMarksAllSelectedPublishesReached(t *testing.T) {
	records := recorder.New(io.Discard, 16)
	t.Cleanup(func() { _ = records.Close(context.Background()) })
	duration := time.Millisecond
	p1 := &publish{sequence: 1, decision: engine.Decision{Selected: true, Fault: config.Fault{Action: "close_connection", Duration: &duration}}}
	p2 := &publish{sequence: 2, decision: engine.Decision{Selected: true, Fault: config.Fault{Action: "close_connection", Duration: &duration}}}
	s := &session{server: &Server{records: records}, channels: map[uint16]*channelState{1: {pending: map[uint64]*publish{1: p1, 2: p2}}}}
	keep, err := s.handleConfirm(frame{kind: frameMethod, channel: 1}, 2, true, true)
	if err != nil || keep || !p1.event.Reached || !p2.event.Reached || !p1.event.Applied || p2.event.Applied {
		t.Fatalf("confirm result keep=%t err=%v first=%+v second=%+v", keep, err, p1.event, p2.event)
	}
}
