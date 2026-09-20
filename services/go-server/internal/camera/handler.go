package camera

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func RegisterRoutes(rg *gin.RouterGroup, cat *Catalog) {
	rg.GET("/cameras", func(c *gin.Context) {
		zone := c.Query("zone")
		// corridor := c.Query("corridor") // Not fully implemented filtering in this stub

		var res []Camera
		for _, cam := range cat.All() {
			if zone != "" && cam.Zone != zone {
				continue
			}
			res = append(res, cam)
		}
		c.JSON(http.StatusOK, res)
	})

	rg.GET("/cameras/:id", func(c *gin.Context) {
		id := c.Param("id")
		if cam, ok := cat.ByID(id); ok {
			c.JSON(http.StatusOK, cam)
			return
		}
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
	})

	rg.GET("/zones", func(c *gin.Context) {
		c.JSON(http.StatusOK, cat.Zones())
	})
}
