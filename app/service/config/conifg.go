package get

type Config struct {
	DB string
}

var (
	c = &Config{
		DB: "mysql",
	}
)

func GetConfig() *Config {
	return c
}
