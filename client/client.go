package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/andreich/audio/common/service"
	"github.com/gordonklaus/portaudio"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/grpclog"
)

var (
	address = flag.String("address", "localhost:9876", "Address to send the recording to.")
	length  = flag.Duration("length", 5*time.Second, "How long to record.")
	input   = flag.String("input", "", "String to match in input device.")

	numChannels = flag.Int("num_channels", 1, "How many channels to record.")
	sampleRate  = flag.Int("sample_rate", 44100, "What sample rate to use to record.")

	certificate = flag.String("cert", "client.pem", "What certificate to use to connect to the server.")
)

func Errorf(format string, args ...interface{}) {
	log.Printf(format, args...)
	os.Exit(1)
}

func process(ctx context.Context, stream service.Recorder_RecordClient) (func([]float32, []float32, portaudio.StreamCallbackTimeInfo, portaudio.StreamCallbackFlags), func(context.Context) error) {
	req := &service.RecordRequest{}
	return func(in, _ []float32, timeInfo portaudio.StreamCallbackTimeInfo, flags portaudio.StreamCallbackFlags) {
			log.Printf("IN: %d, tI: %+v, f: %+v", len(in), timeInfo, flags)
			req.Reset()
			req.Sample = append(req.Sample, in...)
			if err := stream.Send(req); err != nil {
				log.Printf("Stream error: %v", err)
			}
		}, func(context.Context) error {
			return stream.CloseSend()
		}
}

func getInputDevice() (*portaudio.DeviceInfo, error) {
	devices, err := portaudio.Devices()
	if err != nil {
		return nil, err
	}
	for _, dev := range devices {
		log.Printf("Device: %+v", dev.Name)
		if *input != "" && strings.Contains(dev.Name, *input) {
			return dev, nil
		}
	}
	return nil, errors.New("no input device found")
}

func main() {
	flag.Parse()

	creds, err := credentials.NewClientTLSFromFile(*certificate, "")
	if err != nil {
		log.Fatalf("could not load credentials from %q: %v", *certificate, err)
	}
	grpclog.V(2)
	conn, err := grpc.Dial(*address, grpc.WithTransportCredentials(creds))
	if err != nil {
		log.Fatalf("could not connect to %q: %v", *address, err)
	}
	defer conn.Close()

	if err := portaudio.Initialize(); err != nil {
		Errorf("Couldn't initialize Portaudio: %v", err)
	}
	defer func() {
		if err := portaudio.Terminate(); err != nil {
			Errorf("Couldn't terminate cleanly Portaudio: %v", err)
		}
	}()

	client := service.NewRecorderClient(conn)
	ctx := context.Background()

	devIn, err := getInputDevice()
	if err != nil {
		Errorf("Couldn't get default input device: %v", err)
	}
	log.Printf("IN: %s (max channels=%d; sample rate=%.2f)", devIn.Name, devIn.MaxInputChannels, devIn.DefaultSampleRate)

	req := &service.RecordRequest{
		Header: &service.RecordRequest_Header{
			NumChannels: int32(*numChannels),
			SampleRate:  float32(*sampleRate),
		},
	}
	rpcstream, err := client.Record(ctx)
	if err != nil {
		log.Fatalf("Stream error: could not set up stream: %v", err)
	}
	if err := rpcstream.Send(req); err != nil {
		log.Fatalf("Could not send initial request: %v", err)
	}

	cb, cleanup := process(ctx, rpcstream)
	stream, err := portaudio.OpenStream(portaudio.StreamParameters{
		Input: portaudio.StreamDeviceParameters{
			Device:   devIn,
			Channels: *numChannels,
			Latency:  devIn.DefaultLowInputLatency,
		},
		SampleRate:      float64(*sampleRate),
		FramesPerBuffer: *sampleRate,
	}, cb)
	if err != nil {
		Errorf("Couldn't open stream: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(*length)
		if err := stream.Stop(); err != nil {
			Errorf("Couldn't stop stream: %v", err)
		}
		if err := cleanup(ctx); err != nil {
			log.Printf("Stream error (cleanup): %v", err)
		}
	}()
	if err := stream.Start(); err != nil {
		Errorf("Couldn't start stream: %v", err)
	}
	wg.Wait()
}
