package api

import (
	"log"
	"net/http"

	"ton-storage-s3-cli/internal/database"
	"ton-storage-s3-cli/internal/s3"
	"ton-storage-s3-cli/internal/ton"

	"github.com/johannesboyne/gofakes3"
)

type S3Server struct {
	server *http.Server
}

func NewS3Server(db *database.DB, tonSvc *ton.Service, rootDir string) *S3Server {

	backend := s3.NewTonBackend(db, tonSvc, rootDir)

	faker := gofakes3.New(backend,
		gofakes3.WithLogger(gofakes3.GlobalLog()),
		gofakes3.WithTimeSource(gofakes3.DefaultTimeSource()),
	)

	return &S3Server{
		server: &http.Server{
			Handler: faker.Server(),
		},
	}
}

func (s *S3Server) Start(addr string) error {
	log.Printf("📦 S3 Gateway running on %s", addr)
	s.server.Addr = addr
	return s.server.ListenAndServe()
}
