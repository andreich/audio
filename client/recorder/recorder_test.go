package recorder

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/andreich/audio/common/service"
	"github.com/golang/protobuf/proto"
	"github.com/gordonklaus/portaudio"
	"google.golang.org/grpc"
)

type fakeClient struct {
	service.RecorderClient

	idx     int
	streams []*fakeStream
	err     error
}

func (f *fakeClient) verify(t *testing.T) {
	if f.idx != len(f.streams) {
		t.Errorf("have %d streams, only %d have been called", len(f.streams), f.idx)
		return
	}
	for _, stream := range f.streams {
		stream.verify(t)
	}
}

func (f *fakeClient) Record(ctx context.Context, _ ...grpc.CallOption) (service.Recorder_RecordClient, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.idx++
	return f.streams[f.idx-1], nil
}

type fakeStream struct {
	service.Recorder_RecordClient

	sent []*service.RecordRequest
	want []*service.RecordRequest
	err  error
}

func (f *fakeStream) verify(t *testing.T) {
	if len(f.sent) != len(f.want) {
		t.Errorf("stream got only %d messages sent, want %d", len(f.sent), len(f.want))
		return
	}
	for i := 0; i < len(f.sent); i++ {
		if !proto.Equal(f.sent[i], f.want[i]) {
			t.Errorf("stream - msg %d - sent %+v, want %+v", i, f.sent[i], f.want[i])
			return
		}
	}
}

func (f *fakeStream) CloseSend() error {
	return nil
}

func (f *fakeStream) Send(req *service.RecordRequest) error {
	if f.err != nil {
		return f.err
	}
	out, _ := proto.Clone(req).(*service.RecordRequest)
	f.sent = append(f.sent, out)
	return nil
}

func mkSamples(numChannels int32, sampleRate, delta float32) []float32 {
	res := make([]float32, int(float32(numChannels)*sampleRate))
	for i := 0; i < len(res); i += int(numChannels) {
		for j := 0; j < int(numChannels); j++ {
			res[i+j] = delta + float32(float64(i)*math.Pow(-1, float64(i+j)))
		}
	}
	return res
}

func TestRecorder(t *testing.T) {
	ctx := context.Background()

	for _, tc := range []struct {
		desc        string
		client      *fakeClient
		calls       [][]float32
		numChannels int32
		sampleRate  float32
		maxLength   time.Duration
	}{{
		desc: "recording shorter than max length",
		client: &fakeClient{
			streams: []*fakeStream{
				&fakeStream{
					want: []*service.RecordRequest{{
						Header: &service.RecordRequest_Header{
							NumChannels: 1,
							SampleRate:  11025,
						},
					}, {
						Sample: mkSamples(1, 11025, 0),
					}, {
						Sample: mkSamples(1, 11025, 10),
					}},
				},
			},
		},
		calls: [][]float32{
			mkSamples(1, 11025, 0),
			mkSamples(1, 11025, 10),
		},
		numChannels: 1,
		sampleRate:  11025,
		maxLength:   3 * time.Second,
	}, {
		desc: "recording equal max length",
		client: &fakeClient{
			streams: []*fakeStream{
				&fakeStream{
					want: []*service.RecordRequest{{
						Header: &service.RecordRequest_Header{
							NumChannels: 1,
							SampleRate:  11025,
						},
					}, {
						Sample: mkSamples(1, 11025, 0),
					}, {
						Sample: mkSamples(1, 11025, 10),
					}},
				},
			},
		},
		calls: [][]float32{
			mkSamples(1, 11025, 0),
			mkSamples(1, 11025, 10),
		},
		numChannels: 1,
		sampleRate:  11025,
		maxLength:   2 * time.Second,
	}, {
		desc: "recording longer than max length 2x",
		client: &fakeClient{
			streams: []*fakeStream{
				&fakeStream{
					want: []*service.RecordRequest{{
						Header: &service.RecordRequest_Header{
							NumChannels: 1,
							SampleRate:  11025,
						},
					}, {
						Sample: mkSamples(1, 11025, 0),
					}, {
						Sample: mkSamples(1, 11025, 10),
					}},
				},
				&fakeStream{
					want: []*service.RecordRequest{{
						Header: &service.RecordRequest_Header{
							NumChannels: 1,
							SampleRate:  11025,
						},
					}, {
						Sample: mkSamples(1, 11025, 100),
					}, {
						Sample: mkSamples(1, 11025, 200),
					}},
				},
				&fakeStream{
					want: []*service.RecordRequest{{
						Header: &service.RecordRequest_Header{
							NumChannels: 1,
							SampleRate:  11025,
						},
					}, {
						Sample: mkSamples(1, 11025, 300),
					}},
				},
			},
		},
		calls: [][]float32{
			mkSamples(1, 11025, 0),
			mkSamples(1, 11025, 10),
			mkSamples(1, 11025, 100),
			mkSamples(1, 11025, 200),
			mkSamples(1, 11025, 300),
		},
		numChannels: 1,
		sampleRate:  11025,
		maxLength:   2 * time.Second,
	}} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			r := New(tc.client, tc.maxLength, tc.numChannels, tc.sampleRate)
			cb := r.Process(ctx)
			for _, call := range tc.calls {
				cb(call, nil, portaudio.StreamCallbackTimeInfo{}, 0)
			}
			r.Close()
			tc.client.verify(t)
		})
	}
}
