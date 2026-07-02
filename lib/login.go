package lib

func Authenticate(username, password string) bool {
	settings, err := GetSettings()
	if err != nil {
		return false
	}
	return username == settings.Admin && password == settings.Password
}
