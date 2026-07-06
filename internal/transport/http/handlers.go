package http

import (
	"github.com/gofiber/fiber/v2"

	"github.com/mmarquet/native-api/internal/domain"
	"github.com/mmarquet/native-api/internal/service"
)

// deps des handlers http
type Handlers struct {
	Projects    *service.ProjectService
	Servers     *service.ServerService
	Deployments *service.DeploymentService
}

// ---- projets ----

func (h *Handlers) createProject(c *fiber.Ctx) error {
	var p domain.Project
	if err := c.BodyParser(&p); err != nil {
		return badRequest(c, err)
	}
	if err := h.Projects.Create(&p); err != nil {
		return fail(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(p)
}

func (h *Handlers) listProjects(c *fiber.Ctx) error {
	items, err := h.Projects.List()
	if err != nil {
		return fail(c, err)
	}
	return c.JSON(items)
}

func (h *Handlers) getProject(c *fiber.Ctx) error {
	p, err := h.Projects.Get(c.Params("id"))
	if err != nil {
		return fail(c, err)
	}
	return c.JSON(p)
}

func (h *Handlers) updateProject(c *fiber.Ctx) error {
	var p domain.Project
	if err := c.BodyParser(&p); err != nil {
		return badRequest(c, err)
	}
	out, err := h.Projects.Update(c.Params("id"), &p)
	if err != nil {
		return fail(c, err)
	}
	return c.JSON(out)
}

func (h *Handlers) deleteProject(c *fiber.Ctx) error {
	if err := h.Projects.Delete(c.Params("id")); err != nil {
		return fail(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handlers) addService(c *fiber.Ctx) error {
	var svc domain.Service
	if err := c.BodyParser(&svc); err != nil {
		return badRequest(c, err)
	}
	out, err := h.Projects.AddService(c.Params("id"), &svc)
	if err != nil {
		return fail(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(out)
}

func (h *Handlers) deleteService(c *fiber.Ctx) error {
	out, err := h.Projects.DeleteService(c.Params("id"), c.Params("name"))
	if err != nil {
		return fail(c, err)
	}
	return c.JSON(out)
}

func (h *Handlers) validateProject(c *fiber.Ctx) error {
	if err := h.Projects.Validate(c.Params("id")); err != nil {
		return badRequest(c, err)
	}
	return c.JSON(fiber.Map{"valid": true})
}

// ---- serveurs ----

func (h *Handlers) createServer(c *fiber.Ctx) error {
	var srv domain.Server
	if err := c.BodyParser(&srv); err != nil {
		return badRequest(c, err)
	}
	if err := h.Servers.Create(&srv); err != nil {
		return fail(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(srv)
}

func (h *Handlers) listServers(c *fiber.Ctx) error {
	items, err := h.Servers.List()
	if err != nil {
		return fail(c, err)
	}
	return c.JSON(items)
}

func (h *Handlers) getServer(c *fiber.Ctx) error {
	srv, err := h.Servers.Get(c.Params("id"))
	if err != nil {
		return fail(c, err)
	}
	return c.JSON(srv)
}

func (h *Handlers) deleteServer(c *fiber.Ctx) error {
	if err := h.Servers.Delete(c.Params("id")); err != nil {
		return fail(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handlers) pingServer(c *fiber.Ctx) error {
	if err := h.Servers.Ping(c.Context(), c.Params("id")); err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(apiError{
			Error: errBody{Code: "ENGINE_UNREACHABLE", Message: err.Error()},
		})
	}
	return c.JSON(fiber.Map{"reachable": true})
}

// ---- déploiements ----

func (h *Handlers) createDeployment(c *fiber.Ctx) error {
	var d domain.Deployment
	if err := c.BodyParser(&d); err != nil {
		return badRequest(c, err)
	}
	out, err := h.Deployments.Create(&d)
	if err != nil {
		return fail(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(out)
}

func (h *Handlers) listDeployments(c *fiber.Ctx) error {
	items, err := h.Deployments.List()
	if err != nil {
		return fail(c, err)
	}
	return c.JSON(items)
}

func (h *Handlers) getDeployment(c *fiber.Ctx) error {
	d, err := h.Deployments.Get(c.Params("id"))
	if err != nil {
		return fail(c, err)
	}
	return c.JSON(d)
}

func (h *Handlers) upDeployment(c *fiber.Ctx) error {
	d, err := h.Deployments.Up(c.Context(), c.Params("id"))
	if err != nil {
		return fail(c, err)
	}
	return c.JSON(d)
}

func (h *Handlers) downDeployment(c *fiber.Ctx) error {
	d, err := h.Deployments.Down(c.Context(), c.Params("id"))
	if err != nil {
		return fail(c, err)
	}
	return c.JSON(d)
}

func (h *Handlers) startDeployment(c *fiber.Ctx) error {
	d, err := h.Deployments.Start(c.Context(), c.Params("id"))
	if err != nil {
		return fail(c, err)
	}
	return c.JSON(d)
}

func (h *Handlers) stopDeployment(c *fiber.Ctx) error {
	d, err := h.Deployments.Stop(c.Context(), c.Params("id"))
	if err != nil {
		return fail(c, err)
	}
	return c.JSON(d)
}

func (h *Handlers) restartDeployment(c *fiber.Ctx) error {
	d, err := h.Deployments.Restart(c.Context(), c.Params("id"))
	if err != nil {
		return fail(c, err)
	}
	return c.JSON(d)
}

func (h *Handlers) statusDeployment(c *fiber.Ctx) error {
	d, err := h.Deployments.Refresh(c.Context(), c.Params("id"))
	if err != nil {
		return fail(c, err)
	}
	return c.JSON(fiber.Map{"id": d.ID, "status": d.Status, "containers": d.Containers})
}

func (h *Handlers) scaleService(c *fiber.Ctx) error {
	var body struct {
		Replicas int `json:"replicas"`
	}
	if err := c.BodyParser(&body); err != nil {
		return badRequest(c, err)
	}
	d, err := h.Deployments.Scale(c.Context(), c.Params("id"), c.Params("name"), body.Replicas)
	if err != nil {
		return fail(c, err)
	}
	return c.JSON(d)
}

func (h *Handlers) serviceLogs(c *fiber.Ctx) error {
	logs, err := h.Deployments.Logs(c.Context(), c.Params("id"), c.Params("name"))
	if err != nil {
		return fail(c, err)
	}
	return c.Type("txt").SendString(logs)
}
