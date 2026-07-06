// domain = les entités métier (persistées + manipulées par l'api)
package domain

import "time"

// états possibles d'un déploiement
const (
	StatusRunning   = "running"
	StatusNotRunning = "not-running"
	StatusPartial   = "partially-running"
	StatusPending   = "pending"
	StatusError     = "error"
)

// var d'env d'un service
// variable=true -> valeur pas figée dans le projet, filée au déploiement
type EnvVar struct {
	Key      string `json:"key"`
	Value    string `json:"value,omitempty"`
	Variable bool   `json:"variable"`
	Required bool   `json:"required"`
}

// expo d'un port de conteneur
// host peut rester à 0 dans le projet + filé au déploiement si variable=true
type PortMapping struct {
	Container int    `json:"container"`
	Host      int    `json:"host,omitempty"`
	Protocol  string `json:"protocol,omitempty"` // tcp par défaut
	Variable  bool   `json:"variable"`
}

// monte un volume nommé du projet dans un service
type VolumeMount struct {
	Volume string `json:"volume"` // ref vers un VolumeDef du projet
	Target string `json:"target"` // chemin dans le conteneur
}

// déclare un volume nommé au niveau projet
type VolumeDef struct {
	Name string `json:"name"`
}

// build d'une image depuis un contexte local (exclusif avec Image)
type BuildSpec struct {
	Context    string `json:"context"`
	Dockerfile string `json:"dockerfile,omitempty"`
}

// service = un composant applicatif d'un projet
type Service struct {
	ID        string        `json:"id" gorm:"primaryKey"`
	ProjectID string        `json:"-" gorm:"index"`
	Name      string        `json:"name"`
	Image     string        `json:"image,omitempty"`
	Build     *BuildSpec    `json:"build,omitempty" gorm:"serializer:json"`
	Env       []EnvVar      `json:"env,omitempty" gorm:"serializer:json"`
	Ports     []PortMapping `json:"ports,omitempty" gorm:"serializer:json"`
	Volumes   []VolumeMount `json:"volumes,omitempty" gorm:"serializer:json"`
	DependsOn []string      `json:"depends_on,omitempty" gorm:"serializer:json"`
	Scale     int           `json:"scale"`
}

// projet = la description abstraite + réutilisable d'une infra multi-conteneurs
type Project struct {
	ID          string      `json:"id" gorm:"primaryKey"`
	Name        string      `json:"name" gorm:"uniqueIndex"`
	Description string      `json:"description"`
	Services    []Service   `json:"services" gorm:"constraint:OnDelete:CASCADE"`
	Volumes     []VolumeDef `json:"volumes,omitempty" gorm:"serializer:json"`
	CreatedAt   time.Time   `json:"created_at"`
}

// serveur = une instance du docker engine (locale ou distante)
type Server struct {
	ID        string    `json:"id" gorm:"primaryKey"`
	Name      string    `json:"name"`
	Host      string    `json:"host"` // unix:///var/run/docker.sock ou tcp://host:2375
	IsLocal   bool      `json:"is_local"`
	CreatedAt time.Time `json:"created_at"`
}

// valeurs concrètes filées au déploiement pour un service
type ServiceParams struct {
	Env   map[string]string `json:"env,omitempty"`
	Ports map[string]int    `json:"ports,omitempty"` // "portConteneur" -> portHôte
}

// déploiement = instanciation d'un projet sur un serveur
type Deployment struct {
	ID         string                   `json:"id" gorm:"primaryKey"`
	Name       string                   `json:"name"`
	ProjectID  string                   `json:"project_id" gorm:"index"`
	ServerID   string                   `json:"server_id" gorm:"index"`
	Params     map[string]ServiceParams `json:"params,omitempty" gorm:"serializer:json"`
	Status     string                   `json:"status"`
	Containers []Container              `json:"containers,omitempty" gorm:"constraint:OnDelete:CASCADE"`
	CreatedAt  time.Time                `json:"created_at"`
}

// container = instance concrète (conteneur docker) rattachée à un déploiement
type Container struct {
	ID           string `json:"id" gorm:"primaryKey"`
	DeploymentID string `json:"-" gorm:"index"`
	ServiceName  string `json:"service_name"`
	Name         string `json:"name"`
	DockerID     string `json:"docker_id"`
	Status       string `json:"status"`
	Health       string `json:"health,omitempty"`
}
