package models

type AuthService interface {
	AuthTestHandler()
	SignUpHandler() error
	AuthHandler() error
	LoginHandler() error
	TokenHandler() error
	UserInfoHandler() (string, error)
	LogoutHandler() error
}
