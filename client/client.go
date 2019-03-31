package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"os"
	"strings"
	"time"

	"github.com/andreich/audio/client/recorder"
	"github.com/andreich/audio/common/service"
	"github.com/gordonklaus/portaudio"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

var (
	address  = flag.String("address", "localhost:9876", "Address to send the recording to.")
	length   = flag.Duration("length", 5*time.Second, "How long should a chunk be.")
	duration = flag.Duration("duration", 20*time.Second, "How long to record.")
	input    = flag.String("input", "", "String to match in input device.")

	numChannels = flag.Int("num_channels", 1, "How many channels to record.")
	sampleRate  = flag.Int("sample_rate", 44100, "What sample rate to use to record.")

	certificate = flag.String("cert", "client.pem", "What certificate to use to connect to the server.")
)

// Errorf is a shortcut for logging and exiting with code 1.
func Errorf(format string, args ...interface{}) {
	log.Printf(format, args...)
	os.Exit(1)
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

	rec := recorder.New(client, *length, int32(*numChannels), float32(*sampleRate))

	stream, err := portaudio.OpenStream(portaudio.StreamParameters{
		Input: portaudio.StreamDeviceParameters{
			Device:   devIn,
			Channels: *numChannels,
			Latency:  devIn.DefaultLowInputLatency,
		},
		SampleRate:      float64(*sampleRate),
		FramesPerBuffer: *sampleRate,
	}, rec.Process(ctx))
	if err != nil {
		Errorf("Couldn't open stream: %v", err)
	}
	if err := stream.Start(); err != nil {
		Errorf("Couldn't start stream: %v", err)
	}
	<-time.After(*duration)
	if err := stream.Stop(); err != nil {
		Errorf("Couldn't stop stream: %v", err)
	}

	if err := rec.Close(); err != nil {
		log.Printf("Stream error (cleanup): %v", err)
	}
}
