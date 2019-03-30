package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/andreich/audio/common/service"
	"github.com/golang/protobuf/proto"
	"github.com/mkb218/gosndfile/sndfile"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

var (
	bind        = flag.String("bind", "localhost:9876", "Address to bind to.")
	certificate = flag.String("cert", "server.pem", "What certificate to use to connect to the server.")
)

type server struct {
	prefix     string
	mu         sync.Mutex
	numClients int
}

func (s *server) newRecording(numChannels int32, sampleRate int32) (*sndfile.File, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	info := &sndfile.Info{
		Channels:   numChannels,
		Samplerate: sampleRate,
		Format:     sndfile.SF_FORMAT_WAV | sndfile.SF_FORMAT_PCM_16,
	}
	out, err := sndfile.Open(fmt.Sprintf("%s-%s-%03d.wav", s.prefix, time.Now().Format("2006-01-02-15-04-05"), s.numClients), sndfile.Write, info)
	s.numClients += 1
	return out, err
}

func (s *server) Record(srv service.Recorder_RecordServer) error {
	var out *sndfile.File
	for {
		in, err := srv.Recv()
		if err != nil {
			log.Printf("Stream error: %v", err)
			break
		}
		if out == nil {
			out, err = s.newRecording(in.GetHeader().GetNumChannels(), int32(in.GetHeader().GetSampleRate()))
			if err != nil {
				return err
			}
			defer out.Close()
		}
		if len(in.GetSample()) > 0 {
			if _, err = out.WriteFrames(in.GetSample()); err != nil {
				return err
			}
		}
		log.Printf("Got request with %d samples: %d bytes", len(in.GetSample()), proto.Size(in))
	}
	return nil
}

func main() {
	flag.Parse()

	creds, err := credentials.NewServerTLSFromFile(*certificate, *certificate)
	if err != nil {
		log.Fatalf("could not load credentials from %q: %v", *certificate, err)
	}
	log.Printf("Creds: %+v", creds.Info())

	lis, err := net.Listen("tcp", *bind)
	if err != nil {
		log.Fatalf("could not listen on %q: %v", *bind, err)
	}

	s := grpc.NewServer(grpc.Creds(creds))
	service.RegisterRecorderServer(s, &server{prefix: "rec"})
	if err := s.Serve(lis); err != nil {
		log.Fatalf("could not serve: %v", err)
	}
}
