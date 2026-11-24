package reports

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// Handler exposes HTTP REST endpoints for submitting and querying verified hazard reports.
type Handler struct {
	service *Service
}

// NewHandler constructs a Handler for the reports service.
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// RegisterRoutes mounts report endpoints onto the Gin router group.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/reports", h.GetReports)
	rg.POST("/reports", h.SubmitReport)
	rg.GET("/reports/:id", h.GetReportByID)
}

// GetReports godoc
// @Summary List active verified and submitted hazard reports
// @Description Returns list of active crowd-sourced hazard reports with verification status and confidence scores.
// @Tags reports
// @Produce json
// @Success 200 {array} reports.HazardReport
// @Router /api/reports [get]
func (h *Handler) GetReports(c *gin.Context) {
	if h.service == nil {
		c.JSON(http.StatusOK, []HazardReport{})
		return
	}
	reports := h.service.GetActiveReports()
	c.JSON(http.StatusOK, reports)
}

// SubmitReport godoc
// @Summary Submit citizen hazard report
// @Description Submits a new citizen hazard report (flood, congestion, obstruction) at a camera or GPS coordinate.
// @Tags reports
// @Accept json
// @Produce json
// @Param request body reports.SubmitReportRequest true "Hazard Report Payload"
// @Success 201 {object} reports.HazardReport
// @Failure 400 {object} map[string]string "Bad Request"
// @Router /api/reports [post]
func (h *Handler) SubmitReport(c *gin.Context) {
	var req SubmitReportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}

	req.CameraID = strings.TrimSpace(req.CameraID)
	req.ClientIPHash = c.ClientIP()

	report, err := h.service.SubmitReport(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, report)
}

// GetReportByID godoc
// @Summary Get hazard report details by ID
// @Description Returns verification details for a specific hazard report.
// @Tags reports
// @Produce json
// @Param id path string true "Report ID"
// @Success 200 {object} reports.HazardReport
// @Failure 404 {object} map[string]string "Report Not Found"
// @Router /api/reports/{id} [get]
func (h *Handler) GetReportByID(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	report, ok := h.service.GetReport(id)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "report not found"})
		return
	}
	c.JSON(http.StatusOK, report)
}
