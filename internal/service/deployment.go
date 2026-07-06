package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/mmarquet/native-api/internal/docker"
	"github.com/mmarquet/native-api/internal/domain"
)

// labels collés sur toutes les ressources créées -> sert pour la supervision
const (
	labelProject    = "api.project"
	labelDeployment = "api.deployment"
	labelService    = "api.service"
)

// orchestre l'instanciation + le cycle de vie des déploiements
type DeploymentService struct {
	db      *gorm.DB
	manager *docker.Manager
	projects *ProjectService
}

func NewDeploymentService(db *gorm.DB, m *docker.Manager, p *ProjectService) *DeploymentService {
	return &DeploymentService{db: db, manager: m, projects: p}
}

// crée un déploiement (sans l'instancier)
func (s *DeploymentService) Create(d *domain.Deployment) (*domain.Deployment, error) {
	if _, err := s.projects.Get(d.ProjectID); err != nil {
		return nil, fmt.Errorf("projet: %w", err)
	}
	if err := s.db.First(&domain.Server{}, "id = ?", d.ServerID).Error; err != nil {
		return nil, fmt.Errorf("serveur introuvable")
	}
	d.ID = uuid.NewString()
	if d.Name == "" {
		d.Name = "dep-" + d.ID[:8]
	}
	d.Status = domain.StatusPending
	if err := s.db.Create(d).Error; err != nil {
		return nil, err
	}
	return s.Get(d.ID)
}

func (s *DeploymentService) List() ([]domain.Deployment, error) {
	var out []domain.Deployment
	err := s.db.Preload("Containers").Find(&out).Error
	return out, err
}

func (s *DeploymentService) Get(id string) (*domain.Deployment, error) {
	var d domain.Deployment
	err := s.db.Preload("Containers").First(&d, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &d, err
}

func (s *DeploymentService) engineFor(d *domain.Deployment) (*docker.Engine, error) {
	var srv domain.Server
	if err := s.db.First(&srv, "id = ?", d.ServerID).Error; err != nil {
		return nil, fmt.Errorf("serveur introuvable")
	}
	return s.manager.Get(srv)
}

// up = instancie le déploiement -> pull/build images, crée volumes + conteneurs
func (s *DeploymentService) Up(ctx context.Context, id string) (*domain.Deployment, error) {
	d, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	project, err := s.projects.Get(d.ProjectID)
	if err != nil {
		return nil, err
	}
	eng, err := s.engineFor(d)
	if err != nil {
		return nil, err
	}
	ordered, err := topoSort(project.Services)
	if err != nil {
		return nil, err
	}

	// réseau privé du déploiement -> les services se joignent par leur nom (alias dns)
	netName := resName(d.Name, "net")
	if err := eng.EnsureNetwork(ctx, netName, s.labels(project.ID, d.ID, "")); err != nil {
		s.setStatus(d.ID, domain.StatusError)
		return nil, err
	}

	// volumes du projet, préfixés par le nom du déploiement
	for _, v := range project.Volumes {
		if err := eng.EnsureVolume(ctx, resName(d.Name, v.Name), s.labels(project.ID, d.ID, "")); err != nil {
			s.setStatus(d.ID, domain.StatusError)
			return nil, err
		}
	}

	for _, svc := range ordered {
		image, err := s.prepareImage(ctx, eng, d, svc)
		if err != nil {
			s.setStatus(d.ID, domain.StatusError)
			return nil, err
		}
		env, err := s.resolveEnv(d, svc)
		if err != nil {
			s.setStatus(d.ID, domain.StatusError)
			return nil, err
		}
		ports := s.resolvePorts(d, svc)
		binds := s.resolveBinds(d, svc)

		scale := svc.Scale
		if scale < 1 {
			scale = 1
		}
		for i := 0; i < scale; i++ {
			name := instanceName(d.Name, svc.Name, i)
			spec := docker.ContainerSpec{
				Name:        name,
				Image:       image,
				Env:         env,
				Ports:       ports,
				Binds:       binds,
				Labels:      s.labels(project.ID, d.ID, svc.Name),
				NetworkName: netName,
				Aliases:     []string{svc.Name},
			}
			dockerID, err := eng.CreateContainer(ctx, spec)
			if err != nil {
				s.setStatus(d.ID, domain.StatusError)
				return nil, err
			}
			if err := eng.StartContainer(ctx, dockerID); err != nil {
				s.setStatus(d.ID, domain.StatusError)
				return nil, err
			}
			s.db.Create(&domain.Container{
				ID:           uuid.NewString(),
				DeploymentID: d.ID,
				ServiceName:  svc.Name,
				Name:         name,
				DockerID:     dockerID,
				Status:       "running",
			})
		}
	}
	return s.Refresh(ctx, d.ID)
}

// pull ou build l'image d'un service, renvoie la ref
func (s *DeploymentService) prepareImage(ctx context.Context, eng *docker.Engine, d *domain.Deployment, svc domain.Service) (string, error) {
	if svc.Build != nil {
		tag := resName(d.Name, svc.Name) + ":latest"
		if err := eng.BuildImage(ctx, svc.Build.Context, svc.Build.Dockerfile, tag); err != nil {
			return "", err
		}
		return tag, nil
	}
	if err := eng.PullImage(ctx, svc.Image); err != nil {
		return "", err
	}
	return svc.Image, nil
}

// fusionne les vars du projet avec les valeurs filées au déploiement
func (s *DeploymentService) resolveEnv(d *domain.Deployment, svc domain.Service) ([]string, error) {
	params := d.Params[svc.Name]
	var out []string
	for _, e := range svc.Env {
		value := e.Value
		if e.Variable {
			v, ok := params.Env[e.Key]
			if !ok {
				if e.Required {
					return nil, fmt.Errorf("service %s: variable requise manquante: %s", svc.Name, e.Key)
				}
				continue
			}
			value = v
		}
		out = append(out, e.Key+"="+value)
	}
	return out, nil
}

// calcule les mappings de ports concrets d'un service
func (s *DeploymentService) resolvePorts(d *domain.Deployment, svc domain.Service) []docker.PortBinding {
	params := d.Params[svc.Name]
	var out []docker.PortBinding
	for _, p := range svc.Ports {
		host := p.Host
		if p.Variable {
			if v, ok := params.Ports[strconv.Itoa(p.Container)]; ok {
				host = v
			}
		}
		out = append(out, docker.PortBinding{Container: p.Container, Host: host, Protocol: p.Protocol})
	}
	return out
}

// construit les montages de volumes (volume préfixé -> chemin cible)
func (s *DeploymentService) resolveBinds(d *domain.Deployment, svc domain.Service) []string {
	var out []string
	for _, vm := range svc.Volumes {
		out = append(out, resName(d.Name, vm.Volume)+":"+vm.Target)
	}
	return out
}

// down = stop + vire tous les conteneurs d'un déploiement
func (s *DeploymentService) Down(ctx context.Context, id string) (*domain.Deployment, error) {
	d, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	eng, err := s.engineFor(d)
	if err != nil {
		return nil, err
	}
	containers, err := eng.ListByLabels(ctx, map[string]string{labelDeployment: d.ID})
	if err != nil {
		return nil, err
	}
	for _, c := range containers {
		_ = eng.RemoveContainer(ctx, c.ID)
	}
	// vire le réseau du déploiement (une fois les conteneurs détachés)
	_ = eng.RemoveNetworksByLabels(ctx, map[string]string{labelDeployment: d.ID})
	s.db.Where("deployment_id = ?", d.ID).Delete(&domain.Container{})
	s.setStatus(d.ID, domain.StatusNotRunning)
	return s.Get(d.ID)
}

// start / stop / restart -> pilotent l'état des conteneurs existants
func (s *DeploymentService) Start(ctx context.Context, id string) (*domain.Deployment, error) {
	return s.eachContainer(ctx, id, func(eng *docker.Engine, dockerID string) error {
		return eng.StartContainer(ctx, dockerID)
	})
}

func (s *DeploymentService) Stop(ctx context.Context, id string) (*domain.Deployment, error) {
	return s.eachContainer(ctx, id, func(eng *docker.Engine, dockerID string) error {
		return eng.StopContainer(ctx, dockerID)
	})
}

func (s *DeploymentService) Restart(ctx context.Context, id string) (*domain.Deployment, error) {
	if _, err := s.Stop(ctx, id); err != nil {
		return nil, err
	}
	return s.Start(ctx, id)
}

func (s *DeploymentService) eachContainer(ctx context.Context, id string, fn func(*docker.Engine, string) error) (*domain.Deployment, error) {
	d, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	eng, err := s.engineFor(d)
	if err != nil {
		return nil, err
	}
	containers, err := eng.ListByLabels(ctx, map[string]string{labelDeployment: d.ID})
	if err != nil {
		return nil, err
	}
	for _, c := range containers {
		if err := fn(eng, c.ID); err != nil {
			return nil, err
		}
	}
	return s.Refresh(ctx, d.ID)
}

// scale = ajuste le nb d'instances d'un service (phase 2)
func (s *DeploymentService) Scale(ctx context.Context, id, serviceName string, replicas int) (*domain.Deployment, error) {
	if replicas < 0 {
		return nil, fmt.Errorf("le nombre de répliques doit être >= 0")
	}
	d, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	project, err := s.projects.Get(d.ProjectID)
	if err != nil {
		return nil, err
	}
	var target *domain.Service
	for i := range project.Services {
		if project.Services[i].Name == serviceName {
			target = &project.Services[i]
		}
	}
	if target == nil {
		return nil, fmt.Errorf("%w: service %s", ErrNotFound, serviceName)
	}
	eng, err := s.engineFor(d)
	if err != nil {
		return nil, err
	}

	var current []domain.Container
	s.db.Where("deployment_id = ? AND service_name = ?", d.ID, serviceName).Find(&current)

	// on réduit -> vire les instances en trop
	for len(current) > replicas {
		last := current[len(current)-1]
		_ = eng.RemoveContainer(ctx, last.DockerID)
		s.db.Delete(&domain.Container{ID: last.ID})
		current = current[:len(current)-1]
	}
	// on augmente -> crée des instances en plus
	if len(current) < replicas {
		image, err := s.prepareImage(ctx, eng, d, *target)
		if err != nil {
			return nil, err
		}
		env, err := s.resolveEnv(d, *target)
		if err != nil {
			return nil, err
		}
		binds := s.resolveBinds(d, *target)
		// ports publiés uniquement sur la 1re instance -> évite les collisions
		for i := len(current); i < replicas; i++ {
			var ports []docker.PortBinding
			if i == 0 {
				ports = s.resolvePorts(d, *target)
			}
			name := instanceName(d.Name, serviceName, i)
			dockerID, err := eng.CreateContainer(ctx, docker.ContainerSpec{
				Name: name, Image: image, Env: env, Ports: ports, Binds: binds,
				Labels:      s.labels(project.ID, d.ID, serviceName),
				NetworkName: resName(d.Name, "net"),
				Aliases:     []string{serviceName},
			})
			if err != nil {
				return nil, err
			}
			if err := eng.StartContainer(ctx, dockerID); err != nil {
				return nil, err
			}
			s.db.Create(&domain.Container{
				ID: uuid.NewString(), DeploymentID: d.ID, ServiceName: serviceName,
				Name: name, DockerID: dockerID, Status: "running",
			})
		}
	}
	return s.Refresh(ctx, d.ID)
}

// logs d'un service (1re instance)
func (s *DeploymentService) Logs(ctx context.Context, id, serviceName string) (string, error) {
	d, err := s.Get(id)
	if err != nil {
		return "", err
	}
	eng, err := s.engineFor(d)
	if err != nil {
		return "", err
	}
	var c domain.Container
	err = s.db.Where("deployment_id = ? AND service_name = ?", d.ID, serviceName).First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return eng.Logs(ctx, c.DockerID, "200")
}

// interroge l'engine, maj l'état des conteneurs + recalcule le statut
func (s *DeploymentService) Refresh(ctx context.Context, id string) (*domain.Deployment, error) {
	d, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	eng, err := s.engineFor(d)
	if err != nil {
		return nil, err
	}
	live, err := eng.ListByLabels(ctx, map[string]string{labelDeployment: d.ID})
	if err != nil {
		return nil, err
	}
	stateByDockerID := map[string]string{}
	running := 0
	for _, c := range live {
		stateByDockerID[c.ID] = docker.NormalizeState(c.State)
		if c.State == "running" {
			running++
		}
	}
	for i := range d.Containers {
		st, ok := stateByDockerID[d.Containers[i].DockerID]
		if !ok {
			st = "missing"
		}
		d.Containers[i].Status = st
		s.db.Model(&domain.Container{ID: d.Containers[i].ID}).Update("status", st)
	}

	expected := len(d.Containers)
	status := domain.StatusNotRunning
	switch {
	case expected == 0:
		status = domain.StatusNotRunning
	case running == 0:
		status = domain.StatusNotRunning
	case running == expected:
		status = domain.StatusRunning
	default:
		status = domain.StatusPartial
	}
	s.setStatus(d.ID, status)
	d.Status = status
	return d, nil
}

func (s *DeploymentService) setStatus(id, status string) {
	s.db.Model(&domain.Deployment{ID: id}).Update("status", status)
}

func (s *DeploymentService) labels(projectID, deploymentID, service string) map[string]string {
	l := map[string]string{labelProject: projectID, labelDeployment: deploymentID}
	if service != "" {
		l[labelService] = service
	}
	return l
}

// préfixe un nom de ressource par le nom du déploiement
func resName(deployment, name string) string { return deployment + "_" + name }

// nom d'un conteneur -> déploiement_service_index
func instanceName(deployment, service string, index int) string {
	return fmt.Sprintf("%s_%s_%d", deployment, service, index)
}
