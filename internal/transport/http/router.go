package http

import (
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/fiber/v2/middleware/requestid"
)

// monte l'app fiber -> middlewares + routes
func NewApp(h *Handlers, apiKey string) *fiber.App {
	app := fiber.New(fiber.Config{
		AppName:      "native-api",
		ErrorHandler: defaultErrorHandler,
	})

	app.Use(recover.New())
	app.Use(requestid.New())
	app.Use(loggerMiddleware())
	if apiKey != "" {
		app.Use(authMiddleware(apiKey))
	}

	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})

	api := app.Group("/api/v1")

	// projets
	api.Post("/projects", h.createProject)
	api.Get("/projects", h.listProjects)
	api.Get("/projects/:id", h.getProject)
	api.Put("/projects/:id", h.updateProject)
	api.Delete("/projects/:id", h.deleteProject)
	api.Post("/projects/:id/services", h.addService)
	api.Delete("/projects/:id/services/:name", h.deleteService)
	api.Post("/projects/:id/validate", h.validateProject)

	// serveurs
	api.Get("/servers", h.listServers)
	api.Post("/servers", h.createServer)
	api.Get("/servers/:id", h.getServer)
	api.Delete("/servers/:id", h.deleteServer)
	api.Get("/servers/:id/ping", h.pingServer)

	// déploiements
	api.Post("/deployments", h.createDeployment)
	api.Get("/deployments", h.listDeployments)
	api.Get("/deployments/:id", h.getDeployment)
	api.Post("/deployments/:id/up", h.upDeployment)
	api.Post("/deployments/:id/down", h.downDeployment)
	api.Post("/deployments/:id/start", h.startDeployment)
	api.Post("/deployments/:id/stop", h.stopDeployment)
	api.Post("/deployments/:id/restart", h.restartDeployment)
	api.Get("/deployments/:id/status", h.statusDeployment)
	api.Post("/deployments/:id/services/:name/scale", h.scaleService)
	api.Get("/deployments/:id/services/:name/logs", h.serviceLogs)

	return app
}

// log chaque requête (méthode, route, statut, latence)
func loggerMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		start := time.Now()
		err := c.Next()
		slog.Info("request",
			"method", c.Method(),
			"path", c.Path(),
			"status", c.Response().StatusCode(),
			"latency_ms", time.Since(start).Milliseconds(),
			"request_id", c.Locals(requestid.ConfigDefault.ContextKey),
		)
		return err
	}
}

// exige une clé api dans l'en-tête X-API-Key (actif si API_KEY set)
func authMiddleware(key string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if c.Path() == "/health" {
			return c.Next()
		}
		if c.Get("X-API-Key") != key {
			return c.Status(fiber.StatusUnauthorized).JSON(apiError{
				Error: errBody{Code: "UNAUTHORIZED", Message: "clé API invalide ou manquante"},
			})
		}
		return c.Next()
	}
}

func defaultErrorHandler(c *fiber.Ctx, err error) error {
	code := fiber.StatusInternalServerError
	if e, ok := err.(*fiber.Error); ok {
		code = e.Code
	}
	return c.Status(code).JSON(apiError{Error: errBody{Code: "ERROR", Message: err.Error()}})
}
