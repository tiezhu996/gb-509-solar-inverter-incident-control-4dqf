package handler

import (
	"errors"
	"net/http"

	"github.com/blueship581/solar-inverter-incident-control/backend/internal/repository"
	"github.com/blueship581/solar-inverter-incident-control/backend/internal/service"
	"github.com/blueship581/solar-inverter-incident-control/backend/internal/util"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func handleError(c *gin.Context, err error) {
	var batchRejected *service.BatchClaimRejectedError
	switch {
	case errors.As(err, &batchRejected):
		util.FailDetails(c, http.StatusConflict, "batch_claim_rejected",
			"批量认领被拒绝：存在不可认领的编号，整批未发生变更",
			gin.H{"blocks": batchRejected.Blocks})
	case errors.Is(err, gorm.ErrRecordNotFound):
		util.Fail(c, http.StatusNotFound, "not_found", "record was not found")
	case errors.Is(err, repository.ErrVersionConflict):
		util.Fail(c, http.StatusConflict, "version_conflict", "record changed; refresh and retry")
	case errors.Is(err, service.ErrInvalidTransition), errors.Is(err, service.ErrInvalidInput):
		util.Fail(c, http.StatusUnprocessableEntity, "business_rule", err.Error())
	default:
		_ = c.Error(err)
		util.Fail(c, http.StatusInternalServerError, "internal_error", "request could not be completed")
	}
}
