package config

import "os"

// config de l'api -> vient des variables d'env
type Config struct {
	Port       string // port http
	LogLevel   string // debug | info | warn | error
	DBPath     string // fichier sqlite (persistance)
	DockerHost string // engine local (socket ou tcp)
	APIKey     string // si set -> auth par clé (phase 2)
}

// load la conf depuis l'env, defaults sinon
func Load() Config {
	return Config{
		Port:       env("PORT", "8080"),
		LogLevel:   env("LOG_LEVEL", "info"),
		DBPath:     env("DB_PATH", "/data/app.db"),
		DockerHost: env("DOCKER_HOST", ""), // vide -> fromenv (socket par défaut)
		APIKey:     env("API_KEY", ""),
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
