package service

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/mmarquet/native-api/internal/docker"
	"github.com/mmarquet/native-api/internal/domain"
)

// gère les serveurs (instances docker engine)
type ServerService struct {
	db      *gorm.DB
	manager *docker.Manager
}

func NewServerService(db *gorm.DB, m *docker.Manager) *ServerService {
	return &ServerService{db: db, manager: m}
}

// garantit qu'un serveur local existe (auto-créé au démarrage)
func (s *ServerService) EnsureLocal() (*domain.Server, error) {
	var srv domain.Server
	err := s.db.First(&srv, "is_local = ?", true).Error
	if err == nil {
		return &srv, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	srv = domain.Server{ID: uuid.NewString(), Name: "local", IsLocal: true}
	if err := s.db.Create(&srv).Error; err != nil {
		return nil, err
	}
	return &srv, nil
}

// enregistre un serveur distant
func (s *ServerService) Create(srv *domain.Server) error {
	srv.ID = uuid.NewString()
	return s.db.Create(srv).Error
}

func (s *ServerService) List() ([]domain.Server, error) {
	var out []domain.Server
	err := s.db.Find(&out).Error
	return out, err
}

func (s *ServerService) Get(id string) (*domain.Server, error) {
	var srv domain.Server
	err := s.db.First(&srv, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &srv, err
}

func (s *ServerService) Delete(id string) error {
	srv, err := s.Get(id)
	if err != nil {
		return err
	}
	if srv.IsLocal {
		return errors.New("le serveur local ne peut pas être supprimé")
	}
	return s.db.Delete(&domain.Server{ID: id}).Error
}

// ping le docker engine du serveur
func (s *ServerService) Ping(ctx context.Context, id string) error {
	srv, err := s.Get(id)
	if err != nil {
		return err
	}
	eng, err := s.manager.Get(*srv)
	if err != nil {
		return err
	}
	return eng.Ping(ctx)
}
