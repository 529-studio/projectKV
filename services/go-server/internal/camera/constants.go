package camera

// Zone constants representing standardized geographic classification across HCMC and surrounding areas.
const (
	ZoneBaRiaVungTau   = "Bà Rịa - Vũng Tàu"
	ZoneBinhDuong      = "Bình Dương"
	ZoneCatLai         = "Cụm Cảng Cát Lái"
	ZoneQuan7NhaBe     = "Quận 7 - Nhà Bè"
	ZoneThuDuc         = "TP. Thủ Đức"
	ZoneBinhTan        = "Bình Tân - Bình Chánh"
	ZoneQ12HocMonCuChi = "Q.12 - Hóc Môn - Củ Chi"
	ZoneNoiThanh       = "Khu vực Nội thành TP.HCM"
)

// DefaultZones contains the standardized list of valid zone identifiers.
var DefaultZones = []string{
	ZoneBaRiaVungTau,
	ZoneBinhDuong,
	ZoneCatLai,
	ZoneQuan7NhaBe,
	ZoneThuDuc,
	ZoneBinhTan,
	ZoneQ12HocMonCuChi,
	ZoneNoiThanh,
}

// Camera feed operational status constants.
const (
	StatusOnline  = "UP"
	StatusOffline = "DOWN"
)

// Route query parameter and path parameter constants.
const (
	ParamZone     = "zone"
	ParamCorridor = "corridor"
	ParamPhuong   = "phuong"
	ParamID       = "id"
)

// API route paths for the camera module.
const (
	RouteCameras      = "/cameras"
	RouteCameraDetail = "/cameras/:id"
	RouteZones        = "/zones"
	RoutePhuong       = "/phuong"
)

// Error message constants.
const (
	ErrCameraNotFound     = "camera not found"
	ErrInvalidCatalogJSON = "unrecognized camera catalog format"
)
