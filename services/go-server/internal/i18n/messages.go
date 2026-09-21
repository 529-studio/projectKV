package i18n

func GetAlertMessage(lang string, severity string, cameraName string) string {
	if lang == "vi" {
		return "Cảnh báo: " + cameraName + " có mức độ " + severity
	}
	return "Alert: " + cameraName + " is at severity " + severity
}
