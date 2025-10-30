package internal

type AuthRequest struct {
	Username     string `json:"username"`
	Useremail    string `json:"useremail"`
	Password     string `json:"password"`
	ClientId     string `json:"client_id"`
	Redirect     string `json:"redirect"`
	Responsetype string `json:"response_type"`
}
