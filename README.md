# Native API : docker-compose mais en api

> Projet réalisé dans le cadre de mon master développeur full-stack à My Digital School Caen

api rest en go qui pilote le docker engine. un **projet** = une infra décrite (façon `docker-compose.yml`),
un **déploiement** = son instanciation sur un **serveur** (docker engine) avec les valeurs concrètes.

## Démarrage

```bash
docker compose up --build
# api -> http://localhost:8080
curl http://localhost:8080/health
```

socket docker de l'hôte monté dans le conteneur, data persistée dans le volume `api_data` (sqlite).

## Choix techniques

- **go + sdk docker officiel** -> api "native" imposée par la consigne
- **fiber** -> framework http léger, conseillé
- **sqlite** (fichier sur volume) -> relationnel, pas de conteneur en plus, abstrait via gorm
- **projet (abstrait) vs déploiement (concret)** -> on sépare ce qui dépend de l'env
- **slog json + clé api optionnelle** -> logs dès le départ, auth activable via `API_KEY`
- **image docker multi-stage** -> binaire statique `CGO_ENABLED=0`

**truc important** : ce qui est propre à l'env (mots de passe, ports, domaines) n'est **pas** dans le projet.
une var marquée `"variable": true` est filée au moment du **déploiement** -> même projet déployable plusieurs fois avec des configs différentes.

## Routes (`/api/v1`)

projets :

- `POST /projects` -> créer
- `GET /projects` · `GET /projects/:id` -> lister / détailler
- `PUT /projects/:id` -> maj
- `DELETE /projects/:id` -> supprimer (refusé si déployé)
- `POST /projects/:id/services` -> ajouter un service
- `DELETE /projects/:id/services/:name` -> retirer un service
- `POST /projects/:id/validate` -> check (images, noms uniques, cycles)

serveurs (docker engine) :

- `GET /servers` · `GET /servers/:id` -> lister / détailler (le `local` est auto-créé)
- `POST /servers` -> enregistrer un engine distant
- `DELETE /servers/:id` -> supprimer
- `GET /servers/:id/ping` -> tester la co

déploiements :

- `POST /deployments` -> créer (projet + serveur + params)
- `GET /deployments` · `GET /deployments/:id` -> lister / détailler
- `POST /deployments/:id/up` -> instancier (pull/build, volumes, run)
- `POST /deployments/:id/down` -> détruire les conteneurs
- `POST /deployments/:id/{start,stop,restart}` -> cycle de vie
- `GET /deployments/:id/status` -> état `running` / `partially-running` / `not-running`
- `POST /deployments/:id/services/:name/scale` -> scaling (`{"replicas": N}`)
- `GET /deployments/:id/services/:name/logs` -> logs d'un service

## Exemple rapide (wordpress)

```bash
# 1. créer un projet
curl -s -X POST localhost:8080/api/v1/projects -H 'Content-Type: application/json' -d '{
  "name": "wordpress",
  "volumes": [{"name": "db_data"}],
  "services": [
    {"name":"db","image":"mysql:8",
     "env":[{"key":"MYSQL_ROOT_PASSWORD","variable":true,"required":true},
            {"key":"MYSQL_DATABASE","value":"wp"}],
     "volumes":[{"volume":"db_data","target":"/var/lib/mysql"}]},
    {"name":"wordpress","image":"wordpress:latest","depends_on":["db"],
     "ports":[{"container":80,"variable":true}],
     "env":[{"key":"WORDPRESS_DB_HOST","value":"db"}]}
  ]
}'   # -> récup "id" du projet

# 2. récup l'id du serveur local
curl -s localhost:8080/api/v1/servers

# 3. créer un déploiement avec les valeurs concrètes
curl -s -X POST localhost:8080/api/v1/deployments -H 'Content-Type: application/json' -d '{
  "name":"wp-demo","project_id":"<PROJECT_ID>","server_id":"<SERVER_ID>",
  "params":{"db":{"env":{"MYSQL_ROOT_PASSWORD":"secret"}},
            "wordpress":{"ports":{"80":8081}}}
}'   # -> récup "id" du déploiement

# 4. instancier + check l'état
curl -s -X POST localhost:8080/api/v1/deployments/<DEPLOY_ID>/up
curl -s localhost:8080/api/v1/deployments/<DEPLOY_ID>/status
# puis ouvrir http://localhost:8081
```

## Périmètre

- **phase 1 (mvp)** : projets, services/conteneurs, images (pull/build), volumes, vars d'env, ports, dépendances entre services, déploiement local, état.
- **phase 2** : multi-serveurs (engines distants) + scaling.
- **réseau (minimal)** : un bridge par déploiement, chaque conteneur attaché avec un alias dns = nom du service -> les services se joignent par leur nom (`wordpress` -> `db`). du coup l'exemple marche de bout en bout.
- **phase 3 (todo)** : réseaux multiples déclarés dans le projet, secrets, labels.

## Architecture

```
transport/http  ->  service (métier)  ->  storage (sqlite)
                              \-> docker (sdk docker engine)
```

- `internal/domain` — entités (project, service, server, deployment, container)
- `internal/service` — métier (validation, tri topo des deps, résolution des vars, calcul d'état)
- `internal/docker` — wrapper du sdk docker (un client par serveur)
- `internal/transport/http` — routes fiber + middlewares (logs, request-id, auth)
