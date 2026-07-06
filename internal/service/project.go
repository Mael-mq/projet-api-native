// service = la logique métier de l'api
package service

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/mmarquet/native-api/internal/domain"
)

// entité pas trouvée
var ErrNotFound = errors.New("ressource introuvable")

// conflit métier (ex. suppression interdite)
var ErrConflict = errors.New("conflit")

// gère le cycle de vie des projets
type ProjectService struct {
	db *gorm.DB
}

func NewProjectService(db *gorm.DB) *ProjectService { return &ProjectService{db: db} }

// crée un projet + ses services
func (s *ProjectService) Create(p *domain.Project) error {
	p.ID = uuid.NewString()
	assignServiceIDs(p)
	if err := validateProject(p); err != nil {
		return err
	}
	return s.db.Create(p).Error
}

// liste tous les projets + leurs services
func (s *ProjectService) List() ([]domain.Project, error) {
	var out []domain.Project
	err := s.db.Preload("Services").Find(&out).Error
	return out, err
}

// get un projet par son id
func (s *ProjectService) Get(id string) (*domain.Project, error) {
	var p domain.Project
	err := s.db.Preload("Services").First(&p, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &p, err
}

// remplace la description d'un projet (services compris)
func (s *ProjectService) Update(id string, in *domain.Project) (*domain.Project, error) {
	existing, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	in.ID = existing.ID
	assignServiceIDs(in)
	if err := validateProject(in); err != nil {
		return nil, err
	}
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("project_id = ?", id).Delete(&domain.Service{}).Error; err != nil {
			return err
		}
		return tx.Session(&gorm.Session{FullSaveAssociations: true}).Save(in).Error
	})
	if err != nil {
		return nil, err
	}
	return s.Get(id)
}

// supprime un projet, sauf s'il a encore des déploiements
func (s *ProjectService) Delete(id string) error {
	if _, err := s.Get(id); err != nil {
		return err
	}
	var count int64
	s.db.Model(&domain.Deployment{}).Where("project_id = ?", id).Count(&count)
	if count > 0 {
		return fmt.Errorf("%w: le projet possède %d déploiement(s)", ErrConflict, count)
	}
	return s.db.Select("Services").Delete(&domain.Project{ID: id}).Error
}

// ajoute un service à un projet existant
func (s *ProjectService) AddService(projectID string, svc *domain.Service) (*domain.Project, error) {
	p, err := s.Get(projectID)
	if err != nil {
		return nil, err
	}
	svc.ID = uuid.NewString()
	svc.ProjectID = projectID
	if svc.Scale == 0 {
		svc.Scale = 1
	}
	p.Services = append(p.Services, *svc)
	if err := validateProject(p); err != nil {
		return nil, err
	}
	if err := s.db.Create(svc).Error; err != nil {
		return nil, err
	}
	return s.Get(projectID)
}

// retire un service d'un projet
func (s *ProjectService) DeleteService(projectID, name string) (*domain.Project, error) {
	if _, err := s.Get(projectID); err != nil {
		return nil, err
	}
	res := s.db.Where("project_id = ? AND name = ?", projectID, name).Delete(&domain.Service{})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, ErrNotFound
	}
	return s.Get(projectID)
}

// check la cohérence d'un projet sans le modifier
func (s *ProjectService) Validate(id string) error {
	p, err := s.Get(id)
	if err != nil {
		return err
	}
	return validateProject(p)
}

func assignServiceIDs(p *domain.Project) {
	for i := range p.Services {
		if p.Services[i].ID == "" {
			p.Services[i].ID = uuid.NewString()
		}
		p.Services[i].ProjectID = p.ID
		if p.Services[i].Scale == 0 {
			p.Services[i].Scale = 1
		}
	}
}

// check: noms uniques + image/build présent + pas de cycles
func validateProject(p *domain.Project) error {
	if p.Name == "" {
		return fmt.Errorf("le projet doit avoir un nom")
	}
	seen := map[string]bool{}
	for _, svc := range p.Services {
		if svc.Name == "" {
			return fmt.Errorf("un service doit avoir un nom")
		}
		if seen[svc.Name] {
			return fmt.Errorf("nom de service dupliqué: %s", svc.Name)
		}
		seen[svc.Name] = true
		if svc.Image == "" && svc.Build == nil {
			return fmt.Errorf("service %s: 'image' ou 'build' est requis", svc.Name)
		}
		if svc.Image != "" && svc.Build != nil {
			return fmt.Errorf("service %s: 'image' et 'build' sont exclusifs", svc.Name)
		}
	}
	// détecte cycle / dépendance inconnue via le tri topo
	_, err := topoSort(p.Services)
	return err
}

// ordonne les services selon depends_on
func topoSort(services []domain.Service) ([]domain.Service, error) {
	byName := make(map[string]domain.Service, len(services))
	for _, s := range services {
		byName[s.Name] = s
	}
	const (
		unvisited = 0
		visiting  = 1
		done      = 2
	)
	state := map[string]int{}
	var order []domain.Service
	var visit func(name string) error
	visit = func(name string) error {
		svc, ok := byName[name]
		if !ok {
			return fmt.Errorf("dépendance inconnue: %s", name)
		}
		switch state[name] {
		case visiting:
			return fmt.Errorf("cycle de dépendances détecté sur le service %s", name)
		case done:
			return nil
		}
		state[name] = visiting
		for _, dep := range svc.DependsOn {
			if err := visit(dep); err != nil {
				return err
			}
		}
		state[name] = done
		order = append(order, svc)
		return nil
	}
	for _, s := range services {
		if err := visit(s.Name); err != nil {
			return nil, err
		}
	}
	return order, nil
}
