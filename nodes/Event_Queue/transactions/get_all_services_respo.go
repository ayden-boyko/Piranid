package internal

// GetAllServicesResponse lists every registered service.
type GetAllServicesResponse struct {
	Services []GetServiceResponse `json:"services"`
}
