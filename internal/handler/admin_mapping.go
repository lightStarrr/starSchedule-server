package handler

import (
	"github.com/google/uuid"

	"github.com/starschedule/server/internal/dto"
	"github.com/starschedule/server/internal/service"
	"github.com/starschedule/server/internal/store/postgres"
)

func serviceProviderPayload(req dto.ProviderRequest, enabled bool) service.ProviderPayload {
	return service.ProviderPayload{
		Name:           req.Name,
		BaseURL:        req.BaseURL,
		APIKey:         req.APIKey,
		Enabled:        enabled,
		TimeoutSeconds: req.TimeoutSeconds,
	}
}

func postgresModelInput(req dto.ModelCreateRequest, providerID uuid.UUID, enabled bool) postgres.ModelInput {
	return postgres.ModelInput{
		ProviderID:    providerID,
		ModelID:       req.ModelID,
		Alias:         req.Alias,
		DisplayName:   req.DisplayName,
		ContextWindow: req.ContextWindow,
		Enabled:       enabled,
	}
}

func postgresModelPatch(req dto.ModelPatchRequest) postgres.ModelPatch {
	return postgres.ModelPatch{
		Alias:         req.Alias,
		DisplayName:   req.DisplayName,
		ContextWindow: req.ContextWindow,
		Enabled:       req.Enabled,
	}
}
