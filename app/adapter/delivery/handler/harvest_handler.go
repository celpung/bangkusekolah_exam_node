package handler

import (
	"net/http"

	delivery_helper "github.com/celpung/bangkusekolah_exam_node/app/adapter/delivery/helper"
	"github.com/celpung/bangkusekolah_exam_node/app/service"
)

// HarvestHandler exposes the runbook's forced-drain trigger.
type HarvestHandler struct{ svc *service.HarvestService }

func NewHarvestHandler(svc *service.HarvestService) *HarvestHandler {
	return &HarvestHandler{svc: svc}
}

// Force runs one synchronous drain now — the runbook's
// POST /internal/v1/harvest/force.
func (h *HarvestHandler) Force(w http.ResponseWriter, r *http.Request) {
	n, err := h.svc.DrainOnce(r.Context())
	if err != nil {
		delivery_helper.HandleError(w, err)
		return
	}
	delivery_helper.Success(w, http.StatusOK, "harvest forced", map[string]int{"harvested": n})
}
