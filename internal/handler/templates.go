package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/llassingan/provessor/internal/model"
	"github.com/llassingan/provessor/internal/repository"
	"github.com/llassingan/provessor/internal/validator"
)

type TemplateHandler struct {
	repo  *repository.TemplateRepository
	audit *repository.AuditLogRepository
}

func NewTemplateHandler(repo *repository.TemplateRepository, audit *repository.AuditLogRepository) *TemplateHandler {
	return &TemplateHandler{repo: repo, audit: audit}
}

type templateListItem struct {
	ID               int64   `json:"id"`
	Name             string  `json:"name"`
	Description      string  `json:"description"`
	Type             string  `json:"type"`
	LogoURL          string  `json:"logo_url,omitempty"`
	Shape            string  `json:"shape"`
	DefaultOCPU      float64 `json:"default_ocpu"`
	DefaultMemory    float64 `json:"default_memory"`
	BootVolumeSizeGB int     `json:"boot_volume_size_gb"`
}

func templateToList(t *model.Template) templateListItem {
	return templateListItem{
		ID:               t.ID,
		Name:             t.Name,
		Description:      t.Description,
		Type:             t.Type,
		LogoURL:          t.LogoURL,
		Shape:            t.Shape,
		DefaultOCPU:      t.DefaultOCPU,
		DefaultMemory:    t.DefaultMemory,
		BootVolumeSizeGB: t.BootVolumeSizeGB,
	}
}

func (h *TemplateHandler) HandleListTemplates(w http.ResponseWriter, r *http.Request) {
	templates, err := h.repo.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list templates")
		return
	}

	items := make([]templateListItem, 0, len(templates))
	for i := range templates {
		items = append(items, templateToList(&templates[i]))
	}
	writeJSON(w, http.StatusOK, items)
}

type createTemplateRequest struct {
	Name             string  `json:"name"`
	Description      string  `json:"description"`
	LogoURL          string  `json:"logo_url,omitempty"`
	CloudInitYAML    string  `json:"cloud_init_yaml"`
	Shape            string  `json:"shape"`
	DefaultOCPU      float64 `json:"default_ocpu"`
	DefaultMemory    float64 `json:"default_memory"`
	BootVolumeSizeGB int     `json:"boot_volume_size_gb"`
}

func (h *TemplateHandler) HandleCreateTemplate(w http.ResponseWriter, r *http.Request) {
	var req createTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	req.Description = strings.TrimSpace(req.Description)
	req.CloudInitYAML = strings.TrimSpace(req.CloudInitYAML)
	req.Shape = strings.TrimSpace(req.Shape)

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.CloudInitYAML == "" {
		writeError(w, http.StatusBadRequest, "cloud_init_yaml is required")
		return
	}
	if req.Shape == "" {
		writeError(w, http.StatusBadRequest, "shape is required")
		return
	}
	if req.DefaultOCPU <= 0 {
		req.DefaultOCPU = 1.0
	}
	if req.DefaultMemory <= 0 {
		req.DefaultMemory = 8.0
	}
	if req.BootVolumeSizeGB <= 0 {
		req.BootVolumeSizeGB = 50
	}

	t := &model.Template{
		Name:             req.Name,
		Description:      req.Description,
		Type:             "custom",
		LogoURL:          req.LogoURL,
		CloudInitYAML:    req.CloudInitYAML,
		Shape:            req.Shape,
		DefaultOCPU:      req.DefaultOCPU,
		DefaultMemory:    req.DefaultMemory,
		BootVolumeSizeGB: req.BootVolumeSizeGB,
	}

	created, err := h.repo.Create(r.Context(), t)
	if err != nil {
		h.audit.Log(r.Context(), model.AuditLog{Operation: "template.create", ResourceType: "template", Status: "failure", ErrorMessage: "failed to create template"})
		writeError(w, http.StatusInternalServerError, "failed to create template")
		return
	}

	h.audit.Log(r.Context(), model.AuditLog{Operation: "template.create", ResourceType: "template", ResourceID: created.ID, Status: "success"})
	writeJSON(w, http.StatusCreated, templateToList(created))
}

func (h *TemplateHandler) HandleListShapes(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, validator.ShapeGroups())
}
