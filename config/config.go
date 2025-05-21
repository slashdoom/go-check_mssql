package config

// Config hold app settigns
type Config struct {
	Verbose  bool
	Server   string
	Port     int
	Username string
	Password string
	Database string
	Timeout  int
	Query    string
	Regex    string
}

var AppConfig Config
