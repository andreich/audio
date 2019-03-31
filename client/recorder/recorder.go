// Package recorder handles the client side of recording a stream and sending
// it to the server.
package recorder

import (
	"context"
	"log"
	"time"

	"github.com/andreich/audio/common/service"
	"github.com/gordonklaus/portaudio"
)

// R is the structure keeping track of current recordings.
type R struct {
	client service.RecorderClient

	currentStream service.Recorder_RecordClient
	currentFrames int64

	numChannels int32
	sampleRate  float32
	maxLength   time.Duration
}

// New creates a new recorder sending per stream up to maxLength data.
func New(client service.RecorderClient, maxLength time.Duration, numChannels int32, sampleRate float32) *R {
	return &R{
		client:      client,
		numChannels: numChannels,
		sampleRate:  sampleRate,
		maxLength:   maxLength,
	}
}

func (r *R) closeCurrentStream() error {
	r.currentFrames = 0
	if r.currentStream == nil {
		return nil
	}
	return r.currentStream.CloseSend()
}

// Close completes the current stream (if it exists).
func (r *R) Close() error {
	return r.closeCurrentStream()
}

func (r *R) testAndCloseStream(ctx context.Context, d time.Duration) error {
	if r.currentStream != nil && r.maxLength >= d {
		return nil
	}
	log.Printf("current stream frames: %d, length %+v", r.currentFrames, d)
	err := r.closeCurrentStream()
	if err != nil {
		log.Printf("ERROR: couldn't close stream: %v", err)
	}
	r.currentStream, err = r.client.Record(ctx)
	if err != nil {
		return err
	}
	return r.currentStream.Send(&service.RecordRequest{
		Header: &service.RecordRequest_Header{
			NumChannels: r.numChannels,
			SampleRate:  r.sampleRate,
		},
	})
}

// Process returns a callback suitable for portaudio.
// From the callback the stream is sent over RPC to the server.
// Based on the sample rate and num channels the current stream duration is
// computed, assuming that one call covers one second of recording time.
func (r *R) Process(ctx context.Context) func([]float32, []float32, portaudio.StreamCallbackTimeInfo, portaudio.StreamCallbackFlags) {
	req := &service.RecordRequest{}
	div := r.sampleRate * float32(r.numChannels)
	return func(in, _ []float32, timeInfo portaudio.StreamCallbackTimeInfo, flags portaudio.StreamCallbackFlags) {
		lin := int64(len(in))
		d := time.Duration(float32(r.currentFrames+lin)/div) * time.Second
		if err := r.testAndCloseStream(ctx, d); err != nil {
			log.Printf("ERROR: couldn't test and close the stream: %v", err)
			log.Printf("ERROR: dropping samples")
			return
		}
		r.currentFrames += lin
		log.Printf("IN: %d, tI: %+v, f: %+v", lin, timeInfo, flags)
		req.Reset()
		req.Sample = append(req.Sample, in...)
		if err := r.currentStream.Send(req); err != nil {
			log.Printf("Stream error: %v", err)
		}
	}
}
