package lib

func Authenticate(username, password string) bool {
	return username == GLOBAL_CONFIG.Settings.Admin && password == GLOBAL_CONFIG.Settings.Password
}
