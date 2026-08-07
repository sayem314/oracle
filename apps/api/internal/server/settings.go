package server

import (
	"github.com/gofiber/fiber/v3"

	"github.com/sayem314/oracle/apps/api/internal/permission"
	"github.com/sayem314/oracle/apps/api/internal/store/db"
)

type settingsResponse struct {
	PermissionDefault string `json:"permission_default"`
	PermissionRules   string `json:"permission_rules"`
	Instructions      string `json:"instructions"`
}

func settingsToResponse(s db.Setting) settingsResponse {
	return settingsResponse{
		PermissionDefault: s.PermissionDefault,
		PermissionRules:   s.PermissionRules,
		Instructions:      s.Instructions,
	}
}

type settingsRequest struct {
	PermissionDefault string `json:"permission_default"`
	PermissionRules   string `json:"permission_rules"`
	Instructions      string `json:"instructions"`
}

func newGetSettingsHandler(deps Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		settings, err := deps.Store.GetSettings(c.Context())
		if err != nil {
			return err
		}
		return c.JSON(settingsToResponse(settings))
	}
}

func newUpsertSettingsHandler(deps Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		var req settingsRequest
		if err := c.Bind().JSON(&req); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
		}
		if _, err := permission.ParseVerdict(req.PermissionDefault); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		}
		if _, err := permission.ParseRules(req.PermissionRules); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		}
		updated, err := deps.Store.UpsertSettings(c.Context(), db.UpsertSettingsParams{
			PermissionDefault: req.PermissionDefault,
			PermissionRules:   req.PermissionRules,
			Instructions:      req.Instructions,
		})
		if err != nil {
			return err
		}

		deps.Chat.SetPermissions(settingsRuleset(updated))
		return c.JSON(settingsToResponse(updated))
	}
}

// settingsRuleset builds the permission Ruleset from stored settings.
func settingsRuleset(s db.Setting) *permission.Ruleset {
	defaultVerdict, _ := permission.ParseVerdict(s.PermissionDefault)
	rules, _ := permission.ParseRules(s.PermissionRules)
	return permission.NewRuleset(defaultVerdict, rules)
}
